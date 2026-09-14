package main

import (
	"fmt"
	"io"
)

func writeResponse(writer io.Writer, status string, body []byte, closeConnection bool) error {
	connection := "keep-alive"
	if closeConnection {
		connection = "close"
	}
	response := []byte(fmt.Sprintf("HTTP/1.1 %s\r\nContent-Length: %d\r\nContent-Type: text/plain\r\nConnection: %s\r\n\r\n%s", status, len(body), connection, body))
	for len(response) > 0 {
		n, err := writer.Write(response)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		response = response[n:]
	}
	return nil
}
