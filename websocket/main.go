package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os" // Import the os package
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

const (
	// --- Hardcoded Fallback Values ---
	// These are used if the corresponding environment variables are not set.
	DEFAULT_ALERT_URL       = "http://localhost:8001/alert"
	DEFAULT_DYNAMODB_TABLE  = "TemperatureReadings" // Default DynamoDB table name
	DEFAULT_DYNAMODB_REGION = "us-east-1"           // Default AWS region

	// --- Hardcoded Fallback AWS Credentials (USE WITH CAUTION) ---
	// These are used if default AWS config loading fails.
	// Replace with your actual Access Key ID and Secret Access Key
	HARDCODED_AWS_ACCESS_KEY_ID     = "YOUR_HARDCODED_ACCESS_KEY_ID"
	HARDCODED_AWS_SECRET_ACCESS_KEY = "YOUR_HARDCODED_SECRET_ACCESS_KEY"
	// ---------------------------------------------------------
)

var (
	alertURL       string
	dynamoDBTable  string
	dynamoDBRegion string
)

// Temperature represents the temperature data with DynamoDB tags
type Temperature struct {
	Temperature float64 `json:"temperature" dynamodbav:"temperature"` // Add dynamodbav tag
	Timestamp   string  `json:"timestamp" dynamodbav:"timestamp"`     // Add dynamodbav tag
}

type WSMessage struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

var (
	dynamoDBClient *dynamodb.Client
	upgrader       = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all connections for simplicity
		},
	}
	clients    = make(map[*websocket.Conn]bool) // Connected clients
	writeMutex sync.Mutex                       // Mutex to protect writes to WebSocket connections
)

func initConfig() {
	// Read ALERT_URL from environment variable with hardcoded fallback
	alertURL = os.Getenv("ALERT_URL")
	if alertURL == "" {
		alertURL = DEFAULT_ALERT_URL
		log.Printf("ALERT_URL environment variable not set, using default: %s", alertURL)
	} else {
		log.Printf("Using ALERT_URL from environment variable: %s", alertURL)
	}

	// Read DYNAMODB_TABLE from environment variable with hardcoded fallback
	dynamoDBTable = os.Getenv("DYNAMODB_TABLE")
	if dynamoDBTable == "" {
		dynamoDBTable = DEFAULT_DYNAMODB_TABLE
		log.Printf("DYNAMODB_TABLE environment variable not set, using default: %s", dynamoDBTable)
	} else {
		log.Printf("Using DYNAMODB_TABLE from environment variable: %s", dynamoDBTable)
	}

	// Read DYNAMODB_REGION from environment variable with hardcoded fallback
	dynamoDBRegion = os.Getenv("DYNAMODB_REGION")
	if dynamoDBRegion == "" {
		dynamoDBRegion = DEFAULT_DYNAMODB_REGION
		log.Printf("DYNAMODB_REGION environment variable not set, using default: %s", dynamoDBRegion)
	} else {
		log.Printf("Using DYNAMODB_REGION from environment variable: %s", dynamoDBRegion)
	}
}

func initDynamoDB() error {
	var cfg aws.Config
	var err error

	// Attempt to load AWS configuration using default methods (env vars, shared files, etc.)
	// config.WithRegion will use the region from the environment variable if set,
	// otherwise it will use the region from shared config or the fallback region provided.
	cfg, err = config.LoadDefaultConfig(context.Background(), config.WithRegion(dynamoDBRegion))

	// Check if loading default config failed or didn't provide credentials
	if err != nil || cfg.Credentials == nil {
		log.Printf("Failed to load default AWS config or credentials (%v), attempting hardcoded fallback...", err)

		// Use hardcoded credentials as a fallback
		creds := credentials.NewStaticCredentialsProvider(HARDCODED_AWS_ACCESS_KEY_ID, HARDCODED_AWS_SECRET_ACCESS_KEY, "")
		cfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithCredentialsProvider(creds), // Use the static provider
			config.WithRegion(dynamoDBRegion),     // Use the determined region
		)
		if err != nil {
			// If hardcoded fallback also fails, return the error
			return err
		}
		log.Println("Successfully loaded AWS config using hardcoded fallback credentials.")
	} else {
		log.Println("Successfully loaded AWS config using default credentials.")
	}

	// Create a DynamoDB client
	dynamoDBClient = dynamodb.NewFromConfig(cfg)

	// Optional: Check if the table exists and create it if not.
	// In a production environment, you might manage table creation outside the application.
	_, err = dynamoDBClient.DescribeTable(context.Background(), &dynamodb.DescribeTableInput{
		TableName: aws.String(dynamoDBTable), // Use the determined table name
	})

	if err != nil {
		// If the error indicates the table doesn't exist, attempt creation.
		// Checking for specific error types is more robust than string comparison.
		if !bytes.Contains([]byte(err.Error()), []byte("ResourceNotFoundException")) {
			log.Printf("Error describing table (might not exist): %v", err)
		}

		log.Printf("Table %s not found, attempting to create...", dynamoDBTable)
		_, createErr := dynamoDBClient.CreateTable(context.Background(), &dynamodb.CreateTableInput{
			TableName: aws.String(dynamoDBTable), // Use the determined table name
			AttributeDefinitions: []types.AttributeDefinition{
				{
					AttributeName: aws.String("timestamp"), // Match the dynamodbav tag
					AttributeType: types.ScalarAttributeTypeS,
				},
				// If you had a sensor ID, you might use it as a partition key
				// {
				// 	AttributeName: aws.String("sensorID"), // Match the dynamodbav tag
				// 	AttributeType: types.ScalarAttributeTypeS,
				// },
			},
			KeySchema: []types.KeySchemaElement{
				{
					AttributeName: aws.String("timestamp"), // Match the dynamodbav tag
					KeyType:       types.KeyTypeHash,       // Partition key
				},
				// If using SensorID as partition key, Timestamp would be the sort key
				// {
				// 	AttributeName: aws.String("timestamp"), // Match the dynamodbav tag
				// 	KeyType:       types.KeyTypeRange, // Sort key
				// },
			},
			ProvisionedThroughput: &types.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(5), // Adjust as needed
				WriteCapacityUnits: aws.Int64(5), // Adjust as needed
			},
		})
		if createErr != nil {
			return createErr
		}
		log.Printf("Table %s created successfully.", dynamoDBTable)

		// Wait for the table to become active (optional but good practice)
		waiter := dynamodb.NewTableExistsWaiter(dynamoDBClient)
		err = waiter.Wait(context.Background(), &dynamodb.DescribeTableInput{
			TableName: aws.String(dynamoDBTable), // Use the determined table name
		}, 5*time.Minute) // Adjust wait time as needed
		if err != nil {
			return err
		}
		log.Printf("Table %s is active.", dynamoDBTable)

	} else {
		log.Printf("DynamoDB table %s already exists.", dynamoDBTable)
	}

	return nil
}

// saveTemperature saves the temperature data to DynamoDB
func saveTemperature(temp float64) error {
	// Generate a unique timestamp for the item's primary key
	timestamp := time.Now().UTC().Format(time.RFC3339Nano) // Using nanoseconds for higher uniqueness

	item := Temperature{
		Temperature: temp,
		Timestamp:   timestamp,
	}

	// Marshal the struct into a map of DynamoDB attribute values
	// The dynamodbav tags tell MarshalMap how to map struct fields to attribute names
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}

	// Put the item into the DynamoDB table
	_, err = dynamoDBClient.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String(dynamoDBTable), // Use the determined table name
		Item:      av,
	})
	if err != nil {
		return err
	}

	log.Printf("Temperature data %.2f°C saved to DynamoDB with timestamp %s", temp, timestamp)
	return nil
}

func sendAlert(temp float64) {
	alert := map[string]float64{"temperature": temp}
	jsonData, err := json.Marshal(alert)
	if err != nil {
		log.Printf("Failed to marshal alert data: %v", err)
		return
	}

	resp, err := http.Post(alertURL, "application/json", bytes.NewBuffer(jsonData)) // Use the determined alert URL
	if err != nil {
		log.Printf("Failed to send alert to FastAPI server at %s: %v", alertURL, err)
		return
	}
	defer resp.Body.Close()

	log.Printf("Alert sent to FastAPI server at %s. Status code: %d", alertURL, resp.StatusCode)
}

// Broadcast the temperature data to all connected WebSocket clients
func broadcastTemperature(temp float64) {
	// Generate the timestamp for the current time
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Create the WebSocket message to be sent
	message := WSMessage{
		Type: "temperature",
		Payload: map[string]any{
			"temperature": temp,
			"timestamp":   timestamp, // Add timestamp to the payload
		},
	}

	// Marshal the message to JSON
	messageData, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshalling message: %v", err)
		return
	}

	// Send the message to all connected clients
	writeMutex.Lock()
	defer writeMutex.Unlock()
	for client := range clients {
		if err := client.WriteMessage(websocket.TextMessage, messageData); err != nil {
			log.Printf("Error sending message to client: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}

// WebSocket handler
func wsHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	// Register new client
	clients[conn] = true
	defer delete(clients, conn)

	// Listen for messages from the WebSocket client
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		// Handle incoming temperature data from the WebSocket client (e.g., ESP32)
		var temp Temperature
		if err := json.Unmarshal(message, &temp); err == nil {
			// Only process if it looks like a temperature message
			if temp.Temperature != 0 {
				// Log the received temperature before storing
				log.Printf("Received temperature data via WebSocket: %.2f°C", temp.Temperature)

				// Save temperature to database
				if err := saveTemperature(temp.Temperature); err != nil {
					log.Printf("Database error: %v", err)
					// Continue processing other messages even if saving fails
				}

				// Send alert if temperature is abnormal
				if temp.Temperature > 40 || temp.Temperature < 15 {
					go sendAlert(temp.Temperature)
				}

				// Broadcast the temperature to all connected clients
				go broadcastTemperature(temp.Temperature)
			}
		}
	}
}

// Handle HTTP POST route to receive temperature data as a fallback
func temperaturePostHandler(w http.ResponseWriter, r *http.Request) {
	var temp Temperature
	// Decode the incoming JSON body into the Temperature struct
	if err := json.NewDecoder(r.Body).Decode(&temp); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Log the received temperature
	log.Printf("Received temperature data via HTTP POST: %.2f°C", temp.Temperature)

	// Save temperature to the database
	if err := saveTemperature(temp.Temperature); err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Failed to save temperature data", http.StatusInternalServerError)
		return
	}

	// Send alert if temperature is abnormal
	if temp.Temperature > 40 || temp.Temperature < 15 {
		go sendAlert(temp.Temperature)
	}

	// Respond with a success message
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Temperature data saved successfully"))
}

func main() {
	_ = godotenv.Load()
	// Initialize configuration (including reading environment variables)
	initConfig()

	// Initialize DynamoDB
	if err := initDynamoDB(); err != nil {
		log.Fatalf("Failed to initialize DynamoDB: %v", err)
	}

	// Setup WebSocket handler
	http.HandleFunc("/ws", wsHandler)
	// Setup HTTP POST handler for fallback
	http.HandleFunc("/temperature", temperaturePostHandler)

	// Start HTTP server
	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
