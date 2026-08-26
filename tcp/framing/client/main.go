package main

import (
	"encoding/binary"
	"fmt"
	"net"
)

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:8081")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	for _, payload := range [][]byte{[]byte("HELLO"), []byte("WORLD")} {
		if err := writeFrame(conn, payload); err != nil {
			fmt.Println("failed to send message:", err)
			return
		}

		fmt.Println("sent message:", string(payload))
	}
}

func writeFrame(conn net.Conn, payload []byte) error {
	lengthPrefix := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthPrefix, uint32(len(payload)))

	if err := writeAll(conn, lengthPrefix); err != nil {
		return err
	}

	return writeAll(conn, payload)
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}
