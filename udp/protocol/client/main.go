package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/MohanRaj-1/go-network-lab/udp/protocol"
)

func main() {
	req := protocol.Request{
		ID:        42,
		Operation: "TIME",
		Payload:   "",
	}

	data, err := protocol.EncodeRequest(req)
	if err != nil {
		log.Fatal(err)
	}

	serverAddr := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 9001,
	}

	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for attempt := 1; attempt <= 2; attempt++ {
		fmt.Printf("sending request %d (attempt %d)\n", req.ID, attempt)

		_, err = conn.Write(data)
		if err != nil {
			log.Fatal(err)
		}

		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			log.Fatal(err)
		}

		n, err := conn.Read(buf)
		if err != nil {
			fmt.Printf("attempt %d failed: %v\n", attempt, err)
			continue
		}

		response, err := protocol.DecodeResponse(buf[:n])
		if err != nil {
			log.Fatal(err)
		}

		if err := response.ValidateFor(req); err != nil {
			log.Fatal(err)
		}

		fmt.Printf("response: %+v\n", response)
		break
	}
}
