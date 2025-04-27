package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events" // Import Lambda events package
	"github.com/aws/aws-lambda-go/lambda" // Import Lambda core package
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"                  // Use SESv2 for newer API
	sesv2Types "github.com/aws/aws-sdk-go-v2/service/sesv2/types" // Use SESv2 for newer API
)

const (
	// --- Hardcoded Fallback Values ---
	// These are used if the corresponding environment variables are not set.
	DEFAULT_ALERT_DB_TABLE       = "AlertState"               // Default DynamoDB table for alert state
	DEFAULT_AWS_REGION           = "ap-south-1"               // Default AWS region
	DEFAULT_ALERT_INTERVAL_SECS  = 600                        // Default minimum seconds between alerts (10 minutes)
	DEFAULT_ALERT_HIGH_THRESHOLD = 40.0                       // Default *high* temperature threshold for alert
	DEFAULT_ALERT_LOW_THRESHOLD  = 20.0                       // Default *low* temperature threshold for alert (New Constant)
	DEFAULT_SES_SENDER_EMAIL     = "crce.9914.ce@gmail.com"   // Default SES verified sender email
	DEFAULT_SES_RECIPIENT_EMAIL  = "dark03.testing@gmail.com" // Default recipient email

	// --- Hardcoded Fallback AWS Credentials (USE WITH CAUTION) ---
	// These are used if default AWS config loading fails.
	// Replace with your actual Access Key ID and Secret Access Key
	HARDCODED_AWS_ACCESS_KEY_ID     = "YOUR_HARDCODED_ACCESS_KEY_ID"
	HARDCODED_AWS_SECRET_ACCESS_KEY = "YOUR_HARDCODED_SECRET_ACCESS_KEY"
	// ---------------------------------------------------------

	ALERT_STATE_PARTITION_KEY_VALUE = "LastAlert"          // Fixed partition key value for the alert state item
	ALERT_STATE_TIMESTAMP_ATTRIBUTE = "LastAlertTimestamp" // Attribute name for the timestamp
)

var (
	alertDBTable       string
	awsRegion          string
	alertInterval      time.Duration
	alertHighThreshold float64 // Renamed from alertThreshold
	alertLowThreshold  float64 // New variable for low threshold
	sesSenderEmail     string
	sesRecipientEmail  string

	dynamoDBClient *dynamodb.Client
	sesClient      *sesv2.Client
)

// TemperatureData represents the incoming JSON payload
type TemperatureData struct {
	Temperature float64 `json:"temperature"`
}

// AlertStateItem represents the structure of the item in the AlertState DynamoDB table
type AlertStateItem struct {
	Key                string `dynamodbav:"Key"`                // Partition key
	LastAlertTimestamp string `dynamodbav:"LastAlertTimestamp"` // Attribute for the timestamp
}

// init is called once when the Lambda function is initialized.
// Use this for setting up configuration and AWS clients.
func init() {
	// Load configuration from environment variables with fallbacks
	initConfig()

	// Initialize AWS clients
	ctx := context.Background() // Use background context for init
	if err := initAWS(ctx); err != nil {
		// Log the error. The handler should check if clients are nil
		// and return an error response if initialization failed.
		log.Printf("Failed to initialize AWS clients during init: %v", err)
		// Do NOT os.Exit() here, allow the handler to be invoked to return an error response.
	}
}

// initConfig reads configuration values from environment variables with hardcoded fallbacks.
// Now reads both high and low thresholds.
func initConfig() {
	// Read ALERT_DB_TABLE from environment variable with hardcoded fallback
	alertDBTable = os.Getenv("ALERT_DB_TABLE")
	if alertDBTable == "" {
		alertDBTable = DEFAULT_ALERT_DB_TABLE
		log.Printf("ALERT_DB_TABLE environment variable not set, using default: %s", alertDBTable)
	} else {
		log.Printf("Using ALERT_DB_TABLE from environment variable: %s", alertDBTable)
	}

	// Read AWS_REGION from environment variable with hardcoded fallback
	awsRegion = os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = DEFAULT_AWS_REGION
		log.Printf("AWS_REGION environment variable not set, using default: %s", awsRegion)
	} else {
		log.Printf("Using AWS_REGION from environment variable: %s", awsRegion)
	}

	// Read ALERT_INTERVAL from environment variable with hardcoded fallback
	alertIntervalStr := os.Getenv("ALERT_INTERVAL_SECS") // Use SECS suffix for clarity
	if alertIntervalStr != "" {
		interval, err := time.ParseDuration(alertIntervalStr + "s") // Assume value is in seconds
		if err != nil {
			log.Printf("Invalid ALERT_INTERVAL_SECS environment variable '%s', using default: %d seconds", alertIntervalStr, DEFAULT_ALERT_INTERVAL_SECS)
			alertInterval = time.Duration(DEFAULT_ALERT_INTERVAL_SECS) * time.Second
		} else {
			alertInterval = interval
			log.Printf("Using ALERT_INTERVAL_SECS from environment variable: %s", alertInterval)
		}
	} else {
		alertInterval = time.Duration(DEFAULT_ALERT_INTERVAL_SECS) * time.Second
		log.Printf("ALERT_INTERVAL_SECS environment variable not set, using default: %s", alertInterval)
	}

	// Read ALERT_HIGH_THRESHOLD from environment variable with hardcoded fallback (using old name for compatibility or rename)
	alertHighThresholdStr := os.Getenv("ALERT_HIGH_THRESHOLD") // Using a new name for clarity
	if alertHighThresholdStr == "" {
		alertHighThresholdStr = os.Getenv("ALERT_THRESHOLD") // Fallback to the old name if new isn't set
		if alertHighThresholdStr != "" {
			log.Printf("Using ALERT_THRESHOLD environment variable (consider renaming to ALERT_HIGH_THRESHOLD): %s", alertHighThresholdStr)
		}
	}

	if alertHighThresholdStr != "" {
		threshold, err := parseTemperatureThreshold(alertHighThresholdStr)
		if err != nil {
			log.Printf("Invalid ALERT_HIGH_THRESHOLD/ALERT_THRESHOLD environment variable '%s', using default high: %.2f", alertHighThresholdStr, DEFAULT_ALERT_HIGH_THRESHOLD)
			alertHighThreshold = DEFAULT_ALERT_HIGH_THRESHOLD
		} else {
			alertHighThreshold = threshold
			log.Printf("Using ALERT_HIGH_THRESHOLD/ALERT_THRESHOLD from environment variable: %.2f", alertHighThreshold)
		}
	} else {
		alertHighThreshold = DEFAULT_ALERT_HIGH_THRESHOLD
		log.Printf("ALERT_HIGH_THRESHOLD/ALERT_THRESHOLD environment variable not set, using default high: %.2f", alertHighThreshold)
	}

	// Read ALERT_LOW_THRESHOLD from environment variable with hardcoded fallback (New Logic)
	alertLowThresholdStr := os.Getenv("ALERT_LOW_THRESHOLD")
	if alertLowThresholdStr != "" {
		threshold, err := parseTemperatureThreshold(alertLowThresholdStr)
		if err != nil {
			log.Printf("Invalid ALERT_LOW_THRESHOLD environment variable '%s', using default low: %.2f", alertLowThresholdStr, DEFAULT_ALERT_LOW_THRESHOLD)
			alertLowThreshold = DEFAULT_ALERT_LOW_THRESHOLD
		} else {
			alertLowThreshold = threshold
			log.Printf("Using ALERT_LOW_THRESHOLD from environment variable: %.2f", alertLowThreshold)
		}
	} else {
		alertLowThreshold = DEFAULT_ALERT_LOW_THRESHOLD
		log.Printf("ALERT_LOW_THRESHOLD environment variable not set, using default low: %.2f", alertLowThreshold)
	}

	// Read SES_SENDER_EMAIL from environment variable with hardcoded fallback
	sesSenderEmail = os.Getenv("SES_SENDER_EMAIL")
	if sesSenderEmail == "" {
		sesSenderEmail = DEFAULT_SES_SENDER_EMAIL
		log.Printf("SES_SENDER_EMAIL environment variable not set, using default: %s", sesSenderEmail)
	} else {
		log.Printf("Using SES_SENDER_EMAIL from environment variable: %s", sesSenderEmail)
	}

	// Read SES_RECIPIENT_EMAIL from environment variable with hardcoded fallback
	sesRecipientEmail = os.Getenv("SES_RECIPIENT_EMAIL")
	if sesRecipientEmail == "" {
		sesRecipientEmail = DEFAULT_SES_RECIPIENT_EMAIL
		log.Printf("SES_RECIPIENT_EMAIL environment variable not set, using default: %s", sesRecipientEmail)
	} else {
		log.Printf("Using SES_RECIPIENT_EMAIL from environment variable: %s", sesRecipientEmail)
	}

	// Log the active thresholds after loading config
	log.Printf("Active Alert Thresholds: High=%.2f°C, Low=%.2f°C", alertHighThreshold, alertLowThreshold)
}

// Helper function to parse temperature threshold string
func parseTemperatureThreshold(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscan(s, &f)
	return f, err
}

// initAWS initializes the AWS clients (DynamoDB and SES).
func initAWS(ctx context.Context) error {
	// If clients are already initialized, return nil (important for Lambda warm starts)
	if dynamoDBClient != nil && sesClient != nil {
		log.Println("AWS clients already initialized.")
		return nil
	}

	var cfg aws.Config
	var err error

	// Attempt to load AWS configuration using default methods (env vars, shared files, etc.)
	cfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(awsRegion))

	// Check if loading default config failed or didn't provide credentials
	if err != nil || cfg.Credentials == nil {
		log.Printf("Failed to load default AWS config or credentials (%v), attempting hardcoded fallback...", err)

		// Use hardcoded credentials as a fallback
		creds := credentials.NewStaticCredentialsProvider(HARDCODED_AWS_ACCESS_KEY_ID, HARDCODED_AWS_SECRET_ACCESS_KEY, "")
		cfg, err = config.LoadDefaultConfig(ctx,
			config.WithCredentialsProvider(creds), // Use the static provider
			config.WithRegion(awsRegion),          // Use the determined region
		)
		if err != nil {
			// If hardcoded fallback also fails, return the error
			return fmt.Errorf("failed to load AWS config even with hardcoded fallback: %w", err)
		}
		log.Println("Successfully loaded AWS config using hardcoded fallback credentials. WARNING: Using hardcoded credentials is a security risk in production.")
	} else {
		log.Println("Successfully loaded AWS config using default credentials.")
	}

	// Create DynamoDB client
	dynamoDBClient = dynamodb.NewFromConfig(cfg)
	log.Println("DynamoDB client initialized.")

	// Create SES client
	sesClient = sesv2.NewFromConfig(cfg)
	log.Println("SES client initialized.")

	// Optional: Verify table existence (optional for a simple alert state table)
	// _, err = dynamoDBClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
	//		TableName: aws.String(alertDBTable),
	// })
	// if err != nil {
	//		// Log the error but don't fail init. The get/put functions will handle table not found.
	//		log.Printf("Warning: Failed to describe DynamoDB table %s during init: %v", alertDBTable, err)
	// } else {
	//		log.Printf("Successfully connected to DynamoDB table %s.", alertDBTable)
	// }

	return nil
}

// getLastAlertTimestamp retrieves the timestamp of the last alert from DynamoDB.
func getLastAlertTimestamp(ctx context.Context) (time.Time, error) {
	// Ensure DynamoDB client is initialized
	if dynamoDBClient == nil {
		return time.Time{}, fmt.Errorf("DynamoDB client not initialized")
	}

	getItemInput := &dynamodb.GetItemInput{
		TableName: aws.String(alertDBTable),
		Key: map[string]types.AttributeValue{
			"Key": &types.AttributeValueMemberS{Value: ALERT_STATE_PARTITION_KEY_VALUE},
		},
	}

	resp, err := dynamoDBClient.GetItem(ctx, getItemInput)
	if err != nil {
		// Check if the error is a ResourceNotFoundException for the table
		if !bytes.Contains([]byte(err.Error()), []byte("ResourceNotFoundException")) {
			return time.Time{}, fmt.Errorf("error getting alert state from DynamoDB table %s: %w", alertDBTable, err)
		}
		log.Printf("DynamoDB table %s not found. Assuming no previous alert.", alertDBTable)
		return time.Time{}, nil // Return zero time if table not found
	}

	if resp.Item == nil {
		log.Println("No alert state item found in DynamoDB. Assuming no previous alert.")
		return time.Time{}, nil // Return zero time if item not found
	}

	var item AlertStateItem
	err = attributevalue.UnmarshalMap(resp.Item, &item)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to unmarshal alert state item: %w", err)
	}

	// Parse the timestamp string
	lastAlertTime, err := time.Parse(time.RFC3339Nano, item.LastAlertTimestamp) // Assume RFC3339Nano format
	if err != nil {
		log.Printf("Warning: Failed to parse last alert timestamp '%s': %v. Treating as no previous alert.", item.LastAlertTimestamp, err)
		return time.Time{}, nil // Treat unparseable timestamp as no previous alert
	}

	log.Printf("Last alert timestamp retrieved from DB: %s", lastAlertTime.Format(time.RFC3339Nano))
	return lastAlertTime, nil
}

// saveLastAlertTimestamp saves the current timestamp as the last alert time in DynamoDB.
func saveLastAlertTimestamp(ctx context.Context) error {
	// Ensure DynamoDB client is initialized
	if dynamoDBClient == nil {
		return fmt.Errorf("DynamoDB client not initialized")
	}

	now := time.Now().UTC()
	timestampStr := now.Format(time.RFC3339Nano) // Use RFC3339Nano for consistency

	item := AlertStateItem{
		Key:                ALERT_STATE_PARTITION_KEY_VALUE,
		LastAlertTimestamp: timestampStr,
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("failed to marshal alert state item: %w", err)
	}

	putItemInput := &dynamodb.PutItemInput{
		TableName: aws.String(alertDBTable),
		Item:      av,
	}

	_, err = dynamoDBClient.PutItem(ctx, putItemInput)
	if err != nil {
		return fmt.Errorf("error saving alert state to DynamoDB table %s: %w", alertDBTable, err)
	}

	log.Printf("Saved new last alert timestamp to DB: %s", timestampStr)
	return nil
}

// sendAlertEmail sends an email using Amazon SES.
// Modified to accept the alert type (High/Low) and temperature.
func sendAlertEmail(ctx context.Context, alertType string, temperature float64) error {
	// Ensure SES client is initialized
	if sesClient == nil {
		return fmt.Errorf("SES client not initialized")
	}

	var subject, bodyText, bodyHTML string

	switch alertType {
	case "High":
		subject = fmt.Sprintf("Temperature Alert: High (%.2f°C)", temperature)
		bodyText = fmt.Sprintf("High temperature detected: %.2f°C at %s.", temperature, time.Now().UTC().Format(time.RFC3339))
		bodyHTML = fmt.Sprintf("<html><body><h1>Temperature Alert</h1><p><strong>High</strong> temperature detected: <strong>%.2f°C</strong> at %s.</p></body></html>", temperature, time.Now().UTC().Format(time.RFC3339))
	case "Low":
		subject = fmt.Sprintf("Temperature Alert: Low (%.2f°C)", temperature)
		bodyText = fmt.Sprintf("Low temperature detected: %.2f°C at %s.", temperature, time.Now().UTC().Format(time.RFC3339))
		bodyHTML = fmt.Sprintf("<html><body><h1>Temperature Alert</h1><p><strong>Low</strong> temperature detected: <strong>%.2f°C</strong> at %s.</p></body></html>", temperature, time.Now().UTC().Format(time.RFC3339))
	default:
		// Fallback for unexpected types
		subject = fmt.Sprintf("Temperature Alert: %.2f°C", temperature)
		bodyText = fmt.Sprintf("Temperature outside normal range: %.2f°C at %s.", temperature, time.Now().UTC().Format(time.RFC3339))
		bodyHTML = fmt.Sprintf("<html><body><h1>Temperature Alert</h1><p>Temperature outside normal range: <strong>%.2f°C</strong> at %s.</p></body></html>", temperature, time.Now().UTC().Format(time.RFC3339))
	}

	input := &sesv2.SendEmailInput{
		Destination: &sesv2Types.Destination{
			ToAddresses: []string{
				sesRecipientEmail,
			},
		},
		FromEmailAddress: aws.String(sesSenderEmail),
		Content: &sesv2Types.EmailContent{
			Simple: &sesv2Types.Message{
				Subject: &sesv2Types.Content{
					Data: aws.String(subject),
				},
				Body: &sesv2Types.Body{
					Text: &sesv2Types.Content{
						Data: aws.String(bodyText),
					},
					Html: &sesv2Types.Content{
						Data: aws.String(bodyHTML),
					},
				},
			},
		},
	}

	log.Printf("Attempting to send '%s' temperature alert email from %s to %s...", alertType, sesSenderEmail, sesRecipientEmail)
	_, err := sesClient.SendEmail(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to send alert email: %w", err)
	}

	log.Println("Alert email sent successfully.")
	return nil
}

// HandleRequest is the main Lambda function handler.
// It receives an API Gateway Proxy Request and returns an API Gateway Proxy Response.
func HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Check if AWS clients were initialized successfully
	if dynamoDBClient == nil || sesClient == nil {
		log.Println("AWS clients not initialized.")
		return events.APIGatewayProxyResponse{
			StatusCode: 500, // Internal Server Error
			Body:       "Internal server error: AWS clients not initialized.",
		}, nil // Return nil error to Lambda, put error details in the body
	}

	// Decode the incoming JSON body from the API Gateway request
	var data TemperatureData
	if err := json.Unmarshal([]byte(request.Body), &data); err != nil {
		log.Printf("Error decoding request body: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 400, // Bad Request
			Body:       "Invalid request body",
		}, nil
	}

	now := time.Now().UTC()
	log.Printf("[%s] Received temperature data: %.2f°C", now.Format(time.RFC3339), data.Temperature)

	// Determine if an alert condition is met (either high or low)
	isHighAlert := data.Temperature >= alertHighThreshold
	isLowAlert := data.Temperature <= alertLowThreshold

	// Check if *either* alert condition is met
	if isHighAlert || isLowAlert {
		log.Printf("[%s] Temperature outside normal range (High=%.2f°C, Low=%.2f°C): %.2f°C",
			now.Format(time.RFC3339), alertHighThreshold, alertLowThreshold, data.Temperature)

		// Check last alert timestamp for rate limiting
		lastAlertTime, err := getLastAlertTimestamp(ctx) // Use the Lambda context
		if err != nil {
			log.Printf("Error getting last alert timestamp: %v", err)
			// Continue processing, but alert rate limiting might not work correctly
			// depending on the nature of the DB error. For simplicity, we proceed.
		}

		// Check if rate-limited
		if !lastAlertTime.IsZero() && now.Sub(lastAlertTime) < alertInterval {
			log.Printf("[%s] Alert suppressed due to interval. Last alert was at %s.", now.Format(time.RFC3339), lastAlertTime.Format(time.RFC3339Nano))
			responseBody := map[string]string{
				"status":     "Alert suppressed due to interval",
				"last_alert": lastAlertTime.Format(time.RFC3339Nano),
			}
			jsonResponse, _ := json.Marshal(responseBody) // Ignore error, should be fine
			return events.APIGatewayProxyResponse{
				StatusCode: 200, // OK
				Body:       string(jsonResponse),
				Headers:    map[string]string{"Content-Type": "application/json"},
			}, nil
		}

		// If not rate-limited and an alert condition is met, send the alert

		// Determine the type of alert for the email message
		alertType := ""
		if isHighAlert {
			alertType = "High"
		} else if isLowAlert {
			alertType = "Low"
		}
		// Note: If temperature is exactly the high threshold AND exactly the low threshold,
		// it will be treated as 'High'. This is unlikely but worth noting.
		// If thresholds overlap (e.g., High=25, Low=20, data=22), no alert.
		// If thresholds are inverse (High=20, Low=30), alert always triggers between 20 and 30.
		// Ensure High > Low for sensible operation.

		// Save new alert timestamp (before sending email to mark it as sent/attempted)
		if err := saveLastAlertTimestamp(ctx); err != nil {
			log.Printf("Error saving new alert timestamp: %v", err)
			// Continue, but rate limiting might be affected
		}

		// Send alert email
		if err := sendAlertEmail(ctx, alertType, data.Temperature); err != nil {
			log.Printf("Failed to send %s alert email: %v", alertType, err)
			// Return a 500 error response indicating email sending failed
			return events.APIGatewayProxyResponse{
				StatusCode: 500, // Internal Server Error
				Body:       fmt.Sprintf("Error sending %s alert email: %v", alertType, err),
			}, nil
		}

		responseBody := map[string]string{"status": fmt.Sprintf("%s Temperature Alert Sent", alertType)}
		jsonResponse, _ := json.Marshal(responseBody) // Ignore error
		return events.APIGatewayProxyResponse{
			StatusCode: 200, // OK
			Body:       string(jsonResponse),
			Headers:    map[string]string{"Content-Type": "application/json"},
		}, nil

	} else {
		// Temperature is within the defined range (between low and high thresholds)
		log.Printf("[%s] Temperature within range (High=%.2f°C, Low=%.2f°C): %.2f°C",
			now.Format(time.RFC3339), alertHighThreshold, alertLowThreshold, data.Temperature)
		responseBody := map[string]string{"status": "Temperature within range"}
		jsonResponse, _ := json.Marshal(responseBody) // Ignore error
		return events.APIGatewayProxyResponse{
			StatusCode: 200, // OK
			Body:       string(jsonResponse),
			Headers:    map[string]string{"Content-Type": "application/json"},
		}, nil
	}
}

func main() {
	// init() function is called automatically before main()

	// Start the Lambda handler
	lambda.Start(HandleRequest)
}
