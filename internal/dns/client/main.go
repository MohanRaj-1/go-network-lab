package main

import (
	"fmt"
	"net"
	"time"

	"github.com/MohanRaj-1/go-network-lab/internal/dns"
)

func main() {
	question := dns.Question{
		Name:  "example.com",
		Type:  dns.TypeA,
		Class: dns.ClassIN,
	}

	queryID := uint16(0x1234)

	query, err := dns.EncodeQuery(queryID, question)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Query: % x\n", query)

	resolverAddr, err := net.ResolveUDPAddr(
		"udp",
		"1.1.1.1:53",
	)
	if err != nil {
		panic(err)
	}

	conn, err := net.DialUDP("udp", nil, resolverAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(
		time.Now().Add(3 * time.Second),
	); err != nil {
		panic(err)
	}

	_, err = conn.Write(query)
	if err != nil {
		panic(err)
	}

	response := make([]byte, 4096)

	n, err := conn.Read(response)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			fmt.Println("DNS query timed out")
			return
		}

		fmt.Printf("DNS response read failed: %v\n", err)
		return
	}

	fmt.Printf("Response length: %d\n", n)
	fmt.Printf("Response: % x\n", response[:n])
}
