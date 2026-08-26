package main

import (
	"fmt"
	"net"
)

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	fmt.Println("Connected")

	_, err = conn.Write([]byte("HELLO"))
	if err != nil {
		panic(err)
	}

	fmt.Println("Sent HELLO")

	_, err = conn.Write([]byte("WORLD"))
	if err != nil {
		panic(err)
	}

	fmt.Println("Sent WORLD")

}
