package main

import (
	"errors"
	"fmt"
	"io"
	"net"
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
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	reader := &requestReader{reader: conn}
	for {
		request, err := readRequest(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Println("read request:", err)
				// Close after an invalid request: its remaining bytes cannot be trusted.
				if writeErr := writeResponse(conn, "400 Bad Request", []byte("Bad Request"), true); writeErr != nil {
					fmt.Println("write response:", writeErr)
				}
			}
			return
		}
		printRequest(request)
		status, body := route(request)
		closeConnection := shouldClose(request)
		if err := writeResponse(conn, status, body, closeConnection); err != nil {
			fmt.Println("write response:", err)
			return
		}
		if closeConnection {
			return
		}
	}
}

func route(request Request) (string, []byte) {
	switch request.Target {
	case "/":
		if request.Method == "GET" {
			return "200 OK", []byte("Hello World!")
		}
	case "/echo":
		if request.Method == "POST" {
			return "200 OK", request.Body
		}
	default:
		return "404 Not Found", []byte("Not Found")
	}
	return "405 Method Not Allowed", []byte("Method Not Allowed")
}
