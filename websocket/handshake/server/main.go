package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleWebSocket)

	server := &http.Server{
		Addr:              "localhost:8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("WebSocket server listening on http://localhost:8080")
	log.Fatal(server.ListenAndServe())
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("HTTP request: method=%s path=%s proto=%s",
		r.Method, r.URL.Path, r.Proto)

	log.Printf("Upgrade: %q", r.Header.Get("Upgrade"))
	log.Printf("Connection: %q", r.Header.Get("Connection"))
	log.Printf("WebSocket version: %q",
		r.Header.Get("Sec-WebSocket-Version"))

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("WebSocket handshake failed: %v", err)
		return
	}
	defer conn.CloseNow()

	log.Println("WebSocket handshake succeeded")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	messageType, message, err := conn.Read(ctx)
	if err != nil {
		log.Printf("Read message: %v", err)
		return
	}

	log.Printf("Received WebSocket message: %q", message)

	if err := conn.Write(ctx, messageType, []byte("Hello from server")); err != nil {
		log.Printf("Write message: %v", err)
		return
	}

	log.Println("Response message sent")
}
