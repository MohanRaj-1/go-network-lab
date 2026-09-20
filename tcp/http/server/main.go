package main

import (
	"fmt"
	"net"

	"github.com/MohanRaj-1/go-network-lab/internal/rawhttp"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	listener, err := net.Listen("tcp", ":8083")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept:", err)
			continue
		}
		go rawhttp.HandleConnection(conn)
	}
}
