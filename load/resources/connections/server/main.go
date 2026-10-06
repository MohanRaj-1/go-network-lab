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
	addr := flag.String("addr", ":9300", "TCP listen address")
	flag.Parse()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	var active atomic.Int64
	go report(&active)
	fmt.Println("listening on", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}
		active.Add(1)
		go handleConnection(conn, &active)
	}
}

func handleConnection(conn net.Conn, active *atomic.Int64) {
	defer active.Add(-1)
	defer conn.Close()
	var buffer [1]byte
	for {
		// Idle clients leave this goroutine waiting for network data.
		if _, err := conn.Read(buffer[:]); err != nil {
			return
		}
	}
}

func report(active *atomic.Int64) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		// These are Go runtime measurements, not process working set or socket memory.
		fmt.Printf("connections=%d goroutines=%d heap_alloc_bytes=%d stack_inuse_bytes=%d runtime_sys_bytes=%d\n",
			active.Load(), runtime.NumGoroutine(), memory.HeapAlloc, memory.StackInuse, memory.Sys)
	}
}
