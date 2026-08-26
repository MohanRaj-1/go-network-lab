package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const maxMessageSize = 1 << 20 // 1 MB

func main() {
	listener, err := net.Listen("tcp", ":8081")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Println("framing server listening on :8081")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	lengthBuffer := make([]byte, 4)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			fmt.Println("failed to set read deadline:", err)
			return
		}

		_, err := io.ReadFull(conn, lengthBuffer)
		if err != nil {
			if err != io.EOF {
				fmt.Println("failed to read message length:", err)
			}
			return
		}

		messageLength := binary.BigEndian.Uint32(lengthBuffer)

		if messageLength > maxMessageSize {
			fmt.Println("message too large:", messageLength)
			return
		}

		message := make([]byte, messageLength)

		_, err = io.ReadFull(conn, message)
		if err != nil {
			fmt.Println("failed to read message:", err)
			return
		}

		fmt.Printf("received message: %q\n", message)
	}
}
