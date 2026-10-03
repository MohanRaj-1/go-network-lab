package main

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"net"
)

func main() {
	listener, err := net.Listen("tcp", ":9100")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Println("server listening on :9100")
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

	reader := bufio.NewReader(conn)

	for {
		request, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		if request != "WORK\n" {
			return
		}

		result := doWork()

		if _, err := fmt.Fprintf(conn, "DONE %x\n", result[:4]); err != nil {
			return
		}
	}
}

func doWork() [32]byte {
	data := [32]byte{1}

	for i := 0; i < 10_000; i++ {
		data = sha256.Sum256(data[:])
	}

	return data
}
