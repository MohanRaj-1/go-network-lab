package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}

	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			panic(err)
		}

		fmt.Println("client connected")
		fmt.Println("waiting to read from client...")

		go handleConnection(conn)

	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		fmt.Println("failed to set read deadline:", err)
		return
	}

	buffer := make([]byte, 3)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			fmt.Println("read error:", err)
			return
		}

		fmt.Printf("received %q (%d bytes)\n", buffer[:n], n)
	}
}
