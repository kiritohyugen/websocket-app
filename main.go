package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	mongoClient *mongo.Client
	collection  *mongo.Collection // Global variable for collection
)

type Message struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"` // Auto-generated ID by MongoDB
	SenderID    int64              `bson:"senderId"`      // Sender of the message
	RecipientID int64              `bson:"recipientId"`   // Recipient of the message
	Content     string             `bson:"content"`       // The message content
	Timestamp   int64              `bson:"timestamp"`     // Timestamp when the message is sent
}

// InsertMessage inserts a message into MongoDB
func InsertMessage(message Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// // _, err := collection.InsertOne(ctx, message)
	// result, err := coll.InsertOne(
	// 	context.TODO(),
	// 	bson.D{
	// 		{
	// 			"senderId": 1,
	// 			"recipientId": 2,
	// 			"content": "Hello there!",
	// 			"timestamp": 1672531200000
	// 		  }

	// 	}
	// )
	// if err != nil {
	// 	return err
	// }
	// return nil

	// Hardcoded message
	hardcodedMessage := bson.D{
		{"senderId", 1},
		{"recipientId", 2},
		{"content", "Hello there!"},
		{"timestamp", time.Now().Unix()},
	}

	result, err := collection.InsertOne(ctx, hardcodedMessage)
	if err != nil {
		return err
	}

	// Log the inserted ID from the result
	log.Printf("Hardcoded message inserted successfully with ID: %v\n", result.InsertedID)

	log.Println("Hardcoded message inserted successfully!")
	return nil
}

func connectMongoDB() {
	// Set a timeout for the MongoDB connections
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal("MongoDB connection error:", err)
	}
	log.Println("MongoDB connected successfully:", client)

	// Ping the database to verify the connection
	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("MongoDB ping error:", err)
	}

	// Log successful connection
	log.Println("MongoDB pinged successfully:", client)
	// Save the client globally
	mongoClient = client
	collection = client.Database("mydb").Collection("messages") // Initialize the collection
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow connections from any origin
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}
	defer conn.Close()

	// Continuously listen for messages
	for {
		_, messageData, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read Error:", err)
			break
		}

		// // Log the received message
		// log.Printf("Received: %s", message)

		// // Store message in MongoDB
		// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Set a timeout for the insert operation
		// defer cancel()

		// _, err = collection.InsertOne(ctx, bson.M{"message": string(message)})
		// if err != nil {
		// 	log.Println("MongoDB Insert Error:", err)
		// 	break
		// }

		// // Echo the message back to the client
		// if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
		// 	log.Println("Write Error:", err)
		// 	break
		// }

		// Parse the incoming message into the Message struct
		var message Message
		err = json.Unmarshal(messageData, &message)
		if err != nil {
			log.Println("Error parsing message JSON:", err)
			break
		}

		// Set the current timestamp for the message
		message.Timestamp = time.Now().Unix()

		// Insert the message into MongoDB
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
	// Connect to MongoDB
	connectMongoDB()
	defer func() {
		// Clean up the MongoDB connection when the application exits
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(ctx); err != nil {
			log.Fatal("Error disconnecting from MongoDB:", err)
		}
	}()

	// Serve websocket route
	http.HandleFunc("/ws", websocketHandler)

	// Start HTTP server
	log.Println("WebSocket server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
