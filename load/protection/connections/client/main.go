package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"time"
)

func main() {
	clients := flag.Int("clients", 10, "number of sequential connection attempts per wave")
	waves := flag.Int("waves", 1, "number of consecutive connection waves")
	addr := flag.String("addr", "127.0.0.1:9400", "server TCP address")
	duration := flag.Duration("duration", 30*time.Second, "hold duration after admission setup")
	flag.Parse()
	if *clients < 1 || *waves < 1 || *duration <= 0 {
		panic("clients, waves, and duration must be positive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for wave := 1; wave <= *waves && ctx.Err() == nil; wave++ {
		fmt.Printf("wave=%d/%d\n", wave, *waves)
		runWave(ctx, *clients, *addr, *duration)
	}
}

func runWave(ctx context.Context, clients int, addr string, duration time.Duration) {
	// Each wave owns and closes its connections before the next wave starts.
	connections := make([]net.Conn, 0, clients)
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	var attempted, rejected, failed int
	var firstError error
	dialer := net.Dialer{Timeout: 5 * time.Second}
	for i := 0; i < clients && ctx.Err() == nil; i++ {
		attempted++
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			// Limit the admission line rather than buffering arbitrary server data.
			var reply []byte
			reply, err = bufio.NewReaderSize(conn, 16).ReadSlice('\n')
			if err == nil {
				switch string(reply) {
				case "OK\n":
					conn.SetReadDeadline(time.Time{})
					connections = append(connections, conn)
					continue
				case "BUSY\n":
					rejected++
					conn.Close()
					continue
				default:
					err = fmt.Errorf("unexpected admission response %q", reply)
				}
			}
			conn.Close()
		}
		failed++
		if firstError == nil {
			firstError = err
		}
	}
	fmt.Printf("requested=%d successful=%d rejected=%d failed=%d unattempted=%d\n",
		clients, len(connections), rejected, failed, clients-attempted)
	if firstError != nil {
		fmt.Println("first connection error:", firstError)
	}
	if len(connections) == 0 || ctx.Err() != nil {
		return
	}
	fmt.Printf("holding connections for %s; no requests sent; Ctrl+C closes them early\n", duration)
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	fmt.Println("closing connections")
}
