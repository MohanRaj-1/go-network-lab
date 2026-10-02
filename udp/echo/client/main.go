package main

import (
	"fmt"
	"log"
	"net"
	"time"
)

func main() {
	serverAddr := &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 9000,
	}

	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	message := []byte("UDP")

	_, err = conn.Write(message)
	if err != nil {
		log.Fatal(err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("read error:", err)
		return
	}

	fmt.Printf("received %q\n", buf[:n])
}
