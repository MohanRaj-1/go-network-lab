package main

import (
	"fmt"
	"log"
	"net"

	"github.com/MohanRaj-1/go-network-lab/udp/protocol"
)

func main() {
	addr := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 9001,
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Println("UDP protocol server listening on", addr)

	buf := make([]byte, 1024)
	handler := newRequestHandler()

	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal(err)
		}

		response, duplicate, err := handler.handle(clientAddr.String(), buf[:n])
		if err != nil {
			fmt.Printf("invalid request from %s: %v\n", clientAddr, err)
			continue
		}

		if duplicate {
			fmt.Printf("duplicate request from %s: ID=%d\n", clientAddr, response.ID)
		} else {
			fmt.Printf("received request from %s: ID=%d\n", clientAddr, response.ID)
		}

		data, err := protocol.EncodeResponse(response)
		if err != nil {
			log.Printf("encode response for %s: %v", clientAddr, err)
			continue
		}

		if _, err := conn.WriteToUDP(data, clientAddr); err != nil {
			log.Printf("send response to %s: %v", clientAddr, err)
		}
	}
}
