package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/MohanRaj-1/go-network-lab/protocol"
)

const address = ":8082"

func main() {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	fmt.Println("protocol server listening on", address)
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
	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			fmt.Println("set deadline:", err)
			return
		}
		var request protocol.Request
		if err := protocol.Read(conn, &request); err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Println("read request:", err)
			}
			return
		}
		result := dispatch(request, time.Now().UTC())
		if err := protocol.Write(conn, result.response); err != nil {
			fmt.Println("write response:", err)
			return
		}
		if result.closeConnection {
			return
		}
	}
}

type dispatchResult struct {
	response        protocol.Response
	closeConnection bool
}

func dispatch(request protocol.Request, now time.Time) dispatchResult {
	if err := request.Validate(); err != nil {
		return dispatchResult{
			response:        protocol.ErrorResponse(request.ID, err),
			closeConnection: true,
		}
	}
	response := protocol.Response{Version: protocol.Version, ID: request.ID}
	switch request.Type {
	case protocol.TypePing:
		response.Type = protocol.TypePong
	case protocol.TypeEcho:
		response.Type, response.Message = protocol.TypeEcho, request.Message
	case protocol.TypeTime:
		response.Type, response.Value = protocol.TypeTime, now.Format(time.RFC3339)
	}
	return dispatchResult{response: response}
}
