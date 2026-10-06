package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"time"
)

func main() {
	clients := flag.Int("clients", 10, "number of persistent TCP connections")
	addr := flag.String("addr", "127.0.0.1:9300", "server TCP address")
	duration := flag.Duration("duration", 30*time.Second, "hold duration after connection setup")
	flag.Parse()
	if *clients < 1 || *duration <= 0 {
		panic("clients and duration must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	connections := make([]net.Conn, 0, *clients)
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()

	var failed int
	var firstError error
	dialer := net.Dialer{Timeout: 5 * time.Second}
	// Open sequentially to observe held connections without a simultaneous dial burst.
	for i := 0; i < *clients; i++ {
		if ctx.Err() != nil {
			break
		}
		conn, err := dialer.DialContext(ctx, "tcp", *addr)
		if err != nil {
			failed++
			if firstError == nil {
				firstError = err
			}
			continue
		}
		connections = append(connections, conn)
	}
	fmt.Printf("requested=%d successful=%d failed=%d unattempted=%d\n",
		*clients, len(connections), failed, *clients-len(connections)-failed)
	if firstError != nil {
		fmt.Println("first connection error:", firstError)
	}
	if len(connections) == 0 || ctx.Err() != nil {
		return
	}

	fmt.Printf("holding connections for %s; no requests sent; Ctrl+C closes them early\n", *duration)
	timer := time.NewTimer(*duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	fmt.Println("closing connections")
}
