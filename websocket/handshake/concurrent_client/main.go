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

type connectionResult struct {
	clientID int
	err      error
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ready := make(chan connectionResult, clientCount)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := 1; i <= clientCount; i++ {
		wg.Add(1)

		go func(clientID int) {
			defer wg.Done()
			runClient(ctx, clientID, ready, start)
		}(i)
	}

	// Every client reports readiness or a connection error.
	for i := 0; i < clientCount; i++ {
		select {
		case result := <-ready:
			if result.err != nil {
				log.Printf("[main] client %d connection failed; aborting barrier: %v", result.clientID, result.err)
				cancel()
				wg.Wait()
				return
			}
			log.Printf("[main] client %d is ready", result.clientID)
		case <-ctx.Done():
			log.Printf("[main] barrier aborted: %v", ctx.Err())
			cancel()
			wg.Wait()
			return
		}
	}

	log.Println("[main] all clients ready; releasing them together")
	close(start)

	wg.Wait()
	log.Println("[main] all clients finished")
}

func runClient(
	ctx context.Context,
	clientID int,
	ready chan<- connectionResult,
	start <-chan struct{},
) {
	conn, response, err := websocket.Dial(
		ctx,
		"ws://localhost:8080/ws",
		nil,
	)
	if err != nil {
		ready <- connectionResult{clientID: clientID, err: fmt.Errorf("dial: %w", err)}
		return
	}
	defer conn.CloseNow()

	if response.StatusCode != http.StatusSwitchingProtocols {
		ready <- connectionResult{clientID: clientID, err: fmt.Errorf("unexpected handshake status: %s", response.Status)}
		return
	}

	log.Printf("[client %d] connected; waiting at barrier", clientID)
	ready <- connectionResult{clientID: clientID}

	// No application message is sent until main closes start.
	select {
	case <-start:
		log.Printf("[client %d] released; sending message", clientID)
	case <-ctx.Done():
		log.Printf("[client %d] barrier canceled: %v", clientID, ctx.Err())
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
