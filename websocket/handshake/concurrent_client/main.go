package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const clientCount = 3

func main() {
	ready := make(chan int, clientCount)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := 1; i <= clientCount; i++ {
		wg.Add(1)

		go func(clientID int) {
			defer wg.Done()
			runClient(clientID, ready, start)
		}(i)
	}

	// Wait until every client has established its connection.
	for i := 0; i < clientCount; i++ {
		clientID := <-ready
		log.Printf("[main] client %d is ready", clientID)
	}

	log.Println("[main] all clients ready; releasing them together")
	close(start)

	wg.Wait()
	log.Println("[main] all clients finished")
}

func runClient(
	clientID int,
	ready chan<- int,
	start <-chan struct{},
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	conn, response, err := websocket.Dial(
		ctx,
		"ws://localhost:8080/ws",
		nil,
	)
	if err != nil {
		log.Printf("[client %d] dial failed: %v", clientID, err)
		return
	}
	defer conn.CloseNow()

	if response.StatusCode != http.StatusSwitchingProtocols {
		log.Printf(
			"[client %d] unexpected handshake status: %s",
			clientID,
			response.Status,
		)
		return
	}

	log.Printf("[client %d] connected; waiting at barrier", clientID)
	ready <- clientID

	// No application message is sent until main closes start.
	select {
	case <-start:
		log.Printf("[client %d] released; sending message", clientID)
	case <-ctx.Done():
		log.Printf("[client %d] timed out waiting at barrier", clientID)
		return
	}

	message := fmt.Sprintf("Hello from client %d", clientID)

	if err := conn.Write(
		ctx,
		websocket.MessageText,
		[]byte(message),
	); err != nil {
		log.Printf("[client %d] write failed: %v", clientID, err)
		return
	}

	_, reply, err := conn.Read(ctx)
	if err != nil {
		log.Printf("[client %d] read failed: %v", clientID, err)
		return
	}

	log.Printf("[client %d] received: %s", clientID, reply)

	if err := conn.Close(
		websocket.StatusNormalClosure,
		"done",
	); err != nil {
		log.Printf("[client %d] graceful close failed: %v", clientID, err)
		return
	}

	log.Printf("[client %d] closed gracefully", clientID)
}
