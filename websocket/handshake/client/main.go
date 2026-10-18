package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, response, err := websocket.Dial(
		ctx,
		"ws://localhost:8080/ws",
		nil,
	)
	if err != nil {
		log.Fatalf("WebSocket handshake failed: %v", err)
	}
	defer conn.CloseNow()

	fmt.Printf("Handshake status: %s\n", response.Status)
	fmt.Printf("Response protocol: %s\n", response.Proto)
	fmt.Printf("Upgrade: %s\n", response.Header.Get("Upgrade"))
	fmt.Printf("Connection: %s\n", response.Header.Get("Connection"))
	fmt.Printf("Sec-WebSocket-Accept: %s\n",
		response.Header.Get("Sec-WebSocket-Accept"))

	if response.StatusCode != http.StatusSwitchingProtocols {
		log.Fatalf("expected HTTP 101, got %s", response.Status)
	}

	fmt.Println("WebSocket handshake succeeded")

	if err := conn.Write(ctx, websocket.MessageText, []byte("Hello from client")); err != nil {
		log.Fatalf("send message: %v", err)
	}

	messageType, message, err := conn.Read(ctx)
	if err != nil {
		log.Fatalf("read response: %v", err)
	}

	fmt.Printf("Received message (type=%v): %s\n", messageType, message)
}
