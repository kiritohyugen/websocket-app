package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Message represents the structure of a message document in MongoDB.

var jwtSecretKey []byte // Declare a variable for the JWT secret key

func init() {
	// Retrieve the JWT secret key from the environment variable
	secretKey := os.Getenv("JWT_SECRET_KEY")
	if secretKey == "" {
		log.Fatal("JWT_SECRET_KEY environment variable not set")
	}
	jwtSecretKey = []byte(secretKey)                        // Convert to byte slice
	log.Println("JWT secret key has been set successfully") // Log confirmation
}

type JWTClaims struct {
	ID    int64  `json:"id"`    // Custom claim for user ID
	Level string `json:"level"` // Custom claim for user level
	jwt.RegisteredClaims
}
type Message struct {
	ID          int64  `bson:"_id"`         // Custom sequence ID
	SenderID    int64  `bson:"senderId"`    // Sender of the message
	RecipientID int64  `bson:"recipientId"` // Recipient of the message
	Content     string `bson:"content"`     // The message content
	Timestamp   int64  `bson:"timestamp"`   // Timestamp when the message is sent
}

type IncomingMessage struct {
	Content     string `json:"content"`     // The message content
	SenderID    int64  `json:"senderId"`    // Sender of the message (extracted from claims)
	RecipientID int64  `json:"recipientId"` // Recipient of the message
	Token       string `json:"token"`       // The token received with the message
}

var (
	mongoClient *mongo.Client
	collection  *mongo.Collection // Global variable for collection
	seqColl     *mongo.Collection // Collection for sequence handling
)

// InsertMessage validates the message and inserts it into MongoDB.
func InsertMessage(message Message) error {
	// Validate that SenderID, RecipientID, and Content are non-empty.
	if message.SenderID == 0 || message.RecipientID == 0 || message.Content == "" {
		return errors.New("validation error: senderId, recipientId, and content are required")
	}

	// Retrieve the next value in the sequence for message ID.
	seq, err := getNextSequence("message_sequence")
	if err != nil {
		return err
	}

	// Set the message ID to the next sequence value.
	message.ID = seq
	message.Timestamp = time.Now().Unix()

	// Insert the validated message into MongoDB.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = collection.InsertOne(ctx, message)
	if err != nil {
		return err
	}

	log.Printf("Message inserted successfully with ID: %d", message.ID)
	return nil
}

func getNextSequence(sequenceName string) (int64, error) {
	log.Printf("Fetching next sequence for: %s\n", sequenceName)

	// Define the filter to find the sequence document
	filter := bson.D{{"_id", sequenceName}}

	// Define the update to increment the sequence by 1
	update := bson.D{{"$inc", bson.D{{"sequence", 1}}}}

	// Set the option to return the updated document
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	// Create a map to hold the updated result
	var result bson.M

	// Execute the FindOneAndUpdate operation
	err := seqColl.FindOneAndUpdate(context.TODO(), filter, update, opts).Decode(&result)
	if err != nil {
		log.Printf("Error fetching sequence for %s: %v\n", sequenceName, err)
		return 0, err
	}

	// Log the result of the update
	log.Printf("Sequence document after update: %v\n", result)

	// Extract the "sequence" field as a BSON number
	sequenceVal := result["sequence"]

	var sequence int64
	switch v := sequenceVal.(type) {
	case int32:
		sequence = int64(v)
	case int64:
		sequence = v
	case float64:
		sequence = int64(v)
	default:
		log.Println("Error: Sequence value is not a recognized numeric type")
		return 0, errors.New("sequence value is not a recognized numeric type")
	}

	// Log the successful retrieval of the sequence
	log.Printf("Successfully retrieved next sequence value: %d\n", sequence)

	return sequence, nil
}

func connectMongoDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal("MongoDB connection error:", err)
	}
	log.Println("MongoDB connected successfully")

	// Verify the connection.
	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("MongoDB ping error:", err)
	}

	mongoClient = client
	collection = client.Database("mydb").Collection("messages") // Initialize messages collection
	seqColl = client.Database("mydb").Collection("sequences")   // Initialize sequences collection
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow connections from any origin
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {

	tokenStr := r.URL.Query().Get("token")
	log.Println("token : %s", tokenStr)

	// Validate the token
	claims, err := validateJWTToken(tokenStr)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}
	defer conn.Close()

	for {
		_, messageData, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read Error:", err)
			break
		}

		//	Parse the incoming message into the Message struct

		// Log the raw incoming message data
		log.Printf("Received message data: %s\n", messageData)

		// Parse the incoming message into the Message struct
		var message Message
		err = json.Unmarshal(messageData, &message)
		if err != nil {
			log.Println("Error parsing message JSON:", err)
			log.Printf("Invalid message data: %s\n", messageData)
			break
		}

		// Log the parsed message details
		log.Printf("Parsed message: %+v\n", message)

		// Set SenderID from JWT claims
		message.SenderID = claims.ID
		log.Printf("Assigned SenderID from claims: %d\n", claims.ID)

		// Insert the validated message into MongoDB
		err = InsertMessage(message)
		if err != nil {
			log.Println("MongoDB Insert Error:", err)
			break
		}

		// Echo the message back to the WebSocket client
		if err := conn.WriteMessage(websocket.TextMessage, messageData); err != nil {
			log.Println("Write Error:", err)
			break
		}
	}
}

func validateJWTToken(tokenString string) (*JWTClaims, error) {
	log.Printf("Validating token: %s", tokenString) // Log the token for debugging

	// Decode the base64 encoded secret key
	decodedKey, err := base64.StdEncoding.DecodeString("2Pmtk92MEFb4Mi1ppbEwTRIutN89xTG4GB6S/blXZVA=")
	if err != nil {
		log.Printf("Error decoding secret key: %v", err) // Log decoding errors
		return nil, err
	}

	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return decodedKey, nil // Use the decoded key here
	})

	if err != nil {
		log.Printf("Token parsing error: %v", err) // Log parsing errors
		return nil, err
	}

	// Validate the token and check claims
	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		log.Printf("User ID from JWT: %d", claims.ID)
		log.Printf("User Level from JWT: %s", claims.Level)
		return claims, nil
	} else {
		log.Println("Invalid token claims") // Log invalid claims case
		return nil, errors.New("invalid token")
	}
}

func main() {
	connectMongoDB()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(ctx); err != nil {
			log.Fatal("Error disconnecting from MongoDB:", err)
		}
	}()

	http.HandleFunc("/ws", websocketHandler)

	log.Println("WebSocket server started on :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}
