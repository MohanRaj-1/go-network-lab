package main

import (
	"flag"
	"fmt"
	"net"
	"runtime"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", ":9400", "TCP listen address")
	maximum := flag.Int("max-connections", 100, "maximum admitted connections")
	idleTimeout := flag.Duration("idle-timeout", 5*time.Second, "maximum time allowed without receiving data")
	flag.Parse()
	if *maximum < 1 {
		panic("max-connections must be positive")
	}
	if *idleTimeout <= 0 {
		panic("idle-timeout must be positive")
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	slots := make(chan struct{}, *maximum)
	var active, admitted, rejected atomic.Int64
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Printf("active=%d admitted=%d rejected=%d goroutines=%d\n",
				active.Load(), admitted.Load(), rejected.Load(), runtime.NumGoroutine())
		}
	}()
	fmt.Printf("listening on %s max_connections=%d\n", listener.Addr(), *maximum)
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}
		// TCP establishment precedes this application admission decision.
		select {
		case slots <- struct{}{}:
			active.Add(1)
			admitted.Add(1)
			go handleConnection(conn, slots, &active, *idleTimeout)
		default:
			rejected.Add(1)
			// Bound this write so a rejection cannot indefinitely block Accept.
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			conn.Write([]byte("BUSY\n"))
			conn.Close()
		}
	}
}

func handleConnection(conn net.Conn, slots chan struct{}, active *atomic.Int64, idleTimeout time.Duration) {
	defer func() {
		conn.Close()
		active.Add(-1)
		<-slots
	}()
	// Acknowledgment lets clients count admission rather than just TCP dials.
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("OK\n")); err != nil {
		return
	}
	conn.SetWriteDeadline(time.Time{})
	var buffer [1]byte
	for {
		// Renew after each read: this bounds inactivity, not connection lifetime.
		if err := conn.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return
		}
		if _, err := conn.Read(buffer[:]); err != nil {
			return
		}
	}
}
