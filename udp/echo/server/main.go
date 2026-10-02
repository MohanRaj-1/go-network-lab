package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	addr := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 9000,
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Println("UDP server listening on", addr)

	buf := make([]byte, 6)

	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)

		fmt.Printf("n=%d addr=%v err=%v data=%q\n",
			n,
			clientAddr,
			err,
			buf[:n],
		)

		if err != nil {
			continue
		}

		_, err = conn.WriteToUDP(buf[:n], clientAddr)
		if err != nil {
			log.Fatal(err)
		}
	}
}
