package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/aws/aws-lambda-go/events" // Import Lambda events package
	"github.com/aws/aws-lambda-go/lambda" // Import Lambda core package
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/joho/godotenv"
)

const (
	// --- Hardcoded Fallback Values ---
	// These are used if the corresponding environment variables are not set.
	DEFAULT_DYNAMODB_TABLE  = "TemperatureReadings" // Default DynamoDB table name
	DEFAULT_DYNAMODB_REGION = "ap-south-1"          // Default AWS region

	// --- Hardcoded Fallback AWS Credentials (USE WITH CAUTION) ---
	// These are used if default AWS config loading fails.
	// Replace with your actual Access Key ID and Secret Access Key
	HARDCODED_AWS_ACCESS_KEY_ID     = "YOUR_HARDCODED_ACCESS_KEY_ID"
	HARDCODED_AWS_SECRET_ACCESS_KEY = "YOUR_HARDCODED_SECRET_ACCESS_KEY"
	// ---------------------------------------------------------
)

var (
	dynamoDBTable  string
	dynamoDBRegion string
	dynamoDBClient *dynamodb.Client // Make client global for potential reuse in Lambda environment
)

// Temperature represents the temperature data from DynamoDB
type Temperature struct {
	Temperature float64 `dynamodbav:"temperature"`
	Timestamp   string  `dynamodbav:"timestamp"`
}

// StatsResult holds the calculated statistics
type StatsResult struct {
	Highest float64 `json:"highest"`
	Lowest  float64 `json:"lowest"`
	Mean    float64 `json:"mean"`
	Median  float64 `json:"median"`
	Mode    float64 `json:"mode,omitempty"` // Mode might not always exist, use omitempty
}

// Define the possible time ranges
var timeRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"12h": 12 * time.Hour,
	"1d":  24 * time.Hour,
	"1w":  7 * 24 * time.Hour,
}

// init is called once when the Lambda function is initialized.
// Use this for setting up configuration and the DynamoDB client.
func init() {
	// Initialize configuration (including reading environment variables)
	initConfig()

	// Initialize DynamoDB client
	// Use context.Background() here as the Lambda request context is not available yet
	if err := initDynamoDB(context.Background()); err != nil {
		// Log the error and allow the function to potentially fail on invocation
		// or handle the error in the handler.
		log.Printf("Failed to initialize DynamoDB client during init: %v", err)
		// In a real application, you might want to set a global error variable
		// to check in the handler and return an immediate 500.
	}
}

// initConfig reads configuration values from environment variables with hardcoded fallbacks.
func initConfig() {
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

// initDynamoDB initializes the DynamoDB client, attempting to load credentials
// from default locations (including env vars) and falling back to hardcoded values if necessary.
func initDynamoDB(ctx context.Context) error {
	// If client is already initialized, return nil (important for Lambda warm starts)
	if dynamoDBClient != nil {
		log.Println("DynamoDB client already initialized.")
		return nil
	}

	var cfg aws.Config
	var err error

	// Attempt to load AWS configuration using default methods (env vars, shared files, etc.)
	// config.WithRegion will use the region determined by initConfig().
	cfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(dynamoDBRegion))

	// Check if loading default config failed or didn't provide credentials
	if err != nil || cfg.Credentials == nil {
		log.Printf("Failed to load default AWS config or credentials (%v), attempting hardcoded fallback...", err)

		// Use hardcoded credentials as a fallback
		creds := credentials.NewStaticCredentialsProvider(HARDCODED_AWS_ACCESS_KEY_ID, HARDCODED_AWS_SECRET_ACCESS_KEY, "")
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithCredentialsProvider(creds), // Use the static provider
			config.WithRegion(dynamoDBRegion),     // Use the determined region
		)
		if err != nil {
			// If hardcoded fallback also fails, return the error
			return fmt.Errorf("failed to load AWS config even with hardcoded fallback: %w", err)
		}
		log.Println("Successfully loaded AWS config using hardcoded fallback credentials.")
	} else {
		log.Println("Successfully loaded AWS config using default credentials.")
	}

	// Create a DynamoDB client
	dynamoDBClient = dynamodb.NewFromConfig(cfg)

	// Optional: Verify table existence (optional for a simple stats script)
	// _, err = dynamoDBClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
	// 	TableName: aws.String(dynamoDBTable),
	// })
	// if err != nil {
	// 	// Log the error but don't fail init. The fetch function will handle table not found.
	// 	log.Printf("Warning: Failed to describe DynamoDB table %s during init: %v", dynamoDBTable, err)
	// } else {
	// 	log.Printf("Successfully connected to DynamoDB table %s.", dynamoDBTable)
	// }

	return nil
}

// getTemperaturesInRange fetches temperature data from DynamoDB within a specified time range.
func getTemperaturesInRange(ctx context.Context, rangeKey string) ([]Temperature, error) {
	// Ensure DynamoDB client is initialized
	if dynamoDBClient == nil {
		// This should ideally not happen if init runs successfully, but as a safeguard
		if err := initDynamoDB(ctx); err != nil {
			return nil, fmt.Errorf("DynamoDB client not initialized and failed to re-initialize: %w", err)
		}
	}

	duration, ok := timeRanges[rangeKey]
	if !ok {
		return nil, fmt.Errorf("invalid range key: %s. Valid values are: %v", rangeKey, func() []string {
			keys := make([]string, 0, len(timeRanges))
			for k := range timeRanges {
				keys = append(keys, k)
			}
			return keys
		}())
	}

	endTime := time.Now().UTC()
	startTime := endTime.Add(-duration)

	// Format the start_time to match the RFC3339Nano format used in the Go server
	// This is crucial for the FilterExpression comparison.
	formattedStartTime := startTime.Format(time.RFC3339Nano) // Use RFC3339Nano for consistency

	log.Printf("Fetching data for range: %s (from %s UTC)", rangeKey, formattedStartTime)

	temperatures := []Temperature{}
	lastEvaluatedKey := map[string]types.AttributeValue(nil) // For pagination

	for {
		// Use Scan with a FilterExpression on the timestamp attribute.
		// Note: Scan is less efficient than Query for large tables as it reads all items
		// before filtering. If performance is critical for range queries,
		// consider a different DynamoDB key schema (e.g., Timestamp as Sort Key)
		// or a Global Secondary Index.
		scanInput := &dynamodb.ScanInput{
			TableName:        aws.String(dynamoDBTable), // Use the determined table name
			FilterExpression: aws.String("#ts >= :start_time"),
			ExpressionAttributeNames: map[string]string{
				"#ts": "timestamp", // Attribute name placeholder
			},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":start_time": &types.AttributeValueMemberS{Value: formattedStartTime}, // Attribute value placeholder (S for String)
			},
		}

		if lastEvaluatedKey != nil {
			scanInput.ExclusiveStartKey = lastEvaluatedKey
		}

		resp, err := dynamoDBClient.Scan(ctx, scanInput)
		if err != nil {
			// Check if the error is a ResourceNotFoundException for the table
			if !bytes.Contains([]byte(err.Error()), []byte("ResourceNotFoundException")) {
				return nil, fmt.Errorf("error scanning DynamoDB table %s: %w", dynamoDBTable, err)
			}
			log.Printf("DynamoDB table %s not found.", dynamoDBTable)
			return []Temperature{}, nil // Return empty list if table not found

		}

		// Process the items from the current scan response
		var currentItems []Temperature
		err = attributevalue.UnmarshalListOfMaps(resp.Items, &currentItems)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal DynamoDB items: %w", err)
		}
		temperatures = append(temperatures, currentItems...)

		// Check for pagination
		lastEvaluatedKey = resp.LastEvaluatedKey
		if len(lastEvaluatedKey) == 0 {
			break // No more pages
		}
	}

	log.Printf("Successfully fetched %d temperature readings.", len(temperatures))
	return temperatures, nil
}

// calculateStatistics calculates basic statistics from a slice of Temperature readings.
func calculateStatistics(temperatures []Temperature) *StatsResult {
	if len(temperatures) == 0 {
		log.Println("No temperature data provided for statistics calculation.")
		return nil
	}

	// Extract temperatures into a slice of float64 for easier processing
	tempValues := make([]float64, len(temperatures))
	for i, t := range temperatures {
		tempValues[i] = t.Temperature
	}

	// Calculate Min and Max
	highest := tempValues[0]
	lowest := tempValues[0]
	for _, temp := range tempValues {
		if temp > highest {
			highest = temp
		}
		if temp < lowest {
			lowest = temp
		}
	}

	// Calculate Mean
	sum := 0.0
	for _, temp := range tempValues {
		sum += temp
	}
	mean := sum / float64(len(tempValues))

	// Calculate Median
	sort.Float64s(tempValues)
	median := 0.0
	mid := len(tempValues) / 2
	if len(tempValues)%2 == 0 {
		median = (tempValues[mid-1] + tempValues[mid]) / 2
	} else {
		median = tempValues[mid]
	}

	// Calculate Mode
	// This is a basic implementation for numerical mode.
	// It finds the value(s) that appear most frequently.
	counts := make(map[float64]int)
	maxCount := 0
	for _, temp := range tempValues {
		counts[temp]++
		if counts[temp] > maxCount {
			maxCount = counts[temp]
		}
	}

	// Find all values with the max count
	modes := []float64{}
	for value, count := range counts {
		if count == maxCount {
			modes = append(modes, value)
		}
	}

	// For simplicity, return the first mode found if multiple exist.
	// Or you could return an array of modes depending on requirements.
	mode := math.NaN()                        // Use NaN if no mode (e.g., all values unique) or multiple modes and you only want one
	if maxCount > 1 || len(tempValues) == 1 { // Consider mode only if at least one value repeats or there's only one value
		if len(modes) > 0 {
			mode = modes[0] // Return the first mode
		}
	}

	stats := &StatsResult{
		Highest: highest,
		Lowest:  lowest,
		Mean:    mean,
		Median:  median,
	}

	if !math.IsNaN(mode) {
		stats.Mode = mode
	}

	log.Printf("Calculated statistics: %+v", stats)
	return stats
}

// HandleRequest is the main Lambda function handler.
// It receives an API Gateway Proxy Request and returns an API Gateway Proxy Response.
func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// The 'range' parameter is expected in the query string
	rangeKey, ok := request.QueryStringParameters["range"]
	if !ok || rangeKey == "" {
		// Default to 1 hour if no range parameter is provided
		rangeKey = "1h"
		log.Printf("No 'range' query parameter provided, defaulting to %s", rangeKey)
	} else {
		log.Printf("Using 'range' query parameter: %s", rangeKey)
	}

	// Fetch temperature data from DynamoDB
	temperatures, err := getTemperaturesInRange(ctx, rangeKey)
	if err != nil {
		log.Printf("Error fetching temperature data: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500, // Internal Server Error
			Body:       fmt.Sprintf("Error fetching temperature data: %v", err),
		}, nil // Return nil error to Lambda, put error details in the body
	}

	// Calculate statistics
	stats := calculateStatistics(temperatures)

	// Prepare the response body
	var responseBody []byte
	if stats != nil {
		var marshalErr error
		responseBody, marshalErr = json.Marshal(stats) // Marshal without indent for smaller response
		if marshalErr != nil {
			log.Printf("Error marshalling statistics to JSON: %v", marshalErr)
			return events.APIGatewayProxyResponse{
				StatusCode: 500, // Internal Server Error
				Body:       "Error processing statistics result.",
			}, nil
		}
	} else {
		// No data found, return 404 Not Found
		return events.APIGatewayProxyResponse{
			StatusCode: 404, // Not Found
			Body:       "No temperature data found for the given time range.",
		}, nil
	}

	// Return a successful response
	return events.APIGatewayProxyResponse{
		StatusCode: 200, // OK
		Body:       string(responseBody),
		Headers: map[string]string{
			"Content-Type": "application/json",
			// Add CORS headers if needed, although API Gateway can handle this
			// "Access-Control-Allow-Origin": "*",
		},
	}, nil
}

func main() {
	_ = godotenv.Load() // Load .env file
	// Start the Lambda handler
	lambda.Start(HandleRequest)
}
