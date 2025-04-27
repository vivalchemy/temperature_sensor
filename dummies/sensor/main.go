package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/gorilla/websocket"
)

type Temperature struct {
	TemperatureC float64 `json:"temperature"`
}

type WSMessage struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

func main() {
	// Connect to WebSocket server
	url := "ws://13.127.255.108:8080/ws" // Replace with your WebSocket server URL
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("Error connecting to WebSocket:", err)
	}
	defer conn.Close()

	// Send random temperatures to WebSocket every 2 seconds
	for {
		// Generate random temperature
		temp := Temperature{
			TemperatureC: rand.Float64()*30 + 10, // Random temperature between 10°C and 40°C
		}

		// Send message to WebSocket
		err := conn.WriteJSON(temp)
		if err != nil {
			log.Printf("Error sending message: %v", err)
			break
		}

		fmt.Printf("Sent temperature: %.2f°C\n", temp.TemperatureC)

		// Wait for 2 seconds before sending the next temperature
		time.Sleep(5 * time.Second)
	}
}
