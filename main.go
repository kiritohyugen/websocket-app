package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Message represents the structure of a message document in MongoDB.
type Message struct {
	ID          int64  `bson:"_id"`         // Custom sequence ID
	SenderID    int64  `bson:"senderId"`    // Sender of the message
	RecipientID int64  `bson:"recipientId"` // Recipient of the message
	Content     string `bson:"content"`     // The message content
	Timestamp   int64  `bson:"timestamp"`   // Timestamp when the message is sent
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

// getNextSequence retrieves the next sequence number from the "sequences" collection.
// func getNextSequence(sequenceName string) (int64, error) {

// 	cursor, err := seqColl.Find(context.TODO(), bson.D{{"_id", sequenceName}})

// 	if err != nil {
// 		return 0, err
// 	}

// 	var result bson.M
// 	if cursor.Next(context.TODO()) {
// 		err := cursor.Decode(&result)
// 		if err != nil {
// 			return 0, err
// 		}
// 	}

// 	sequence, ok := result["sequence"].(int64)
// 	if !ok {
// 		// Handle case where sequence is not of type int64
// 		return 0, errors.New("sequence value is not an integer")
// 	}

// 	return sequence, nil

// }

// func getNextSequence(sequenceName string) (int64, error) {
// 	log.Printf("Fetching next sequence for: %s\n", sequenceName)

// 	// Define the filter to find the sequence document
// 	filter := bson.D{{"_id", sequenceName}}

// 	// Define the update to increment the sequence by 1
// 	update := bson.D{{"$inc", bson.D{{"sequence", 1}}}}

// 	// Set the option to return the updated document
// 	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

// 	// Create a map to hold the updated result
// 	var result bson.M

// 	// Execute the FindOneAndUpdate operation
// 	err := seqColl.FindOneAndUpdate(context.TODO(), filter, update, opts).Decode(&result)
// 	if err != nil {
// 		log.Printf("Error fetching sequence for %s: %v\n", sequenceName, err)
// 		return 0, err
// 	}

// 	// Log the result of the update
// 	log.Printf("Sequence document after update: %v\n", result)

// 	// Extract the "sequence" field and assert that it is of type int64
// 	sequence, ok := result["sequence"].(int64)
// 	if !ok {
// 		log.Println("Error: Sequence value is not an integer")
// 		return 0, errors.New("sequence value is not an integer")
// 	}

// 	// Log the successful retrieval of the sequence
// 	log.Printf("Successfully retrieved next sequence value: %d\n", sequence)

// 	return sequence, nil
// }

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

		// Parse the incoming message into the Message struct
		var message Message
		err = json.Unmarshal(messageData, &message)
		if err != nil {
			log.Println("Error parsing message JSON:", err)
			break
		}

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

	log.Println("WebSocket server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
