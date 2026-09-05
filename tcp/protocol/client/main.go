package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/MohanRaj-1/go-network-lab/protocol"
)

const address = "127.0.0.1:8082"

func main() {
	experiments := []struct {
		name string
		run  func() error
	}{
		{"malformed JSON closes without a response", malformedJSON},
		{"missing ECHO message returns ERROR, then closes", missingEchoMessage},
		{"multiple valid requests share one connection", multipleValidRequests},
	}

	for i, experiment := range experiments {
		if err := experiment.run(); err != nil {
			panic(fmt.Sprintf("experiment %d (%s): %v", i+1, experiment.name, err))
		}
		fmt.Printf("PASS %d: %s\n", i+1, experiment.name)
	}
}

func malformedJSON() error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	// The frame itself is valid, but its payload is deliberately invalid JSON.
	payload := []byte(`{"version":1,"id":1,"type":"PING"`)
	if err := writeRawFrame(conn, payload); err != nil {
		return fmt.Errorf("write malformed request: %w", err)
	}

	var response protocol.Response
	if err := protocol.Read(conn, &response); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("received unexpected response: %+v", response)
		}
		return fmt.Errorf("read after malformed JSON: got %v, want EOF", err)
	}
	return nil
}

func missingEchoMessage() error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	request := protocol.Request{Version: protocol.Version, ID: 2, Type: protocol.TypeEcho}
	if err := protocol.Write(conn, request); err != nil {
		return fmt.Errorf("write ECHO request: %w", err)
	}

	var response protocol.Response
	if err := protocol.Read(conn, &response); err != nil {
		return fmt.Errorf("read ERROR response: %w", err)
	}
	if response.Type != protocol.TypeError || response.ID != request.ID {
		return fmt.Errorf("response = %+v, want ERROR for request %d", response, request.ID)
	}

	var next protocol.Response
	if err := protocol.Read(conn, &next); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("received response after ERROR: %+v", next)
		}
		return fmt.Errorf("read after ERROR: got %v, want EOF", err)
	}
	return nil
}

func multipleValidRequests() error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	requests := []protocol.Request{
		{Version: protocol.Version, ID: 3, Type: protocol.TypePing},
		{Version: protocol.Version, ID: 4, Type: protocol.TypeEcho, Message: "hello"},
		{Version: protocol.Version, ID: 5, Type: protocol.TypeTime},
	}
	wantTypes := []string{protocol.TypePong, protocol.TypeEcho, protocol.TypeTime}

	for i, request := range requests {
		if err := protocol.Write(conn, request); err != nil {
			return fmt.Errorf("write request %d: %w", request.ID, err)
		}
		var response protocol.Response
		if err := protocol.Read(conn, &response); err != nil {
			return fmt.Errorf("read response %d: %w", request.ID, err)
		}
		if response.ID != request.ID || response.Type != wantTypes[i] {
			return fmt.Errorf("response = %+v, want id %d and type %s", response, request.ID, wantTypes[i])
		}
		if request.Type == protocol.TypeEcho && response.Message != request.Message {
			return fmt.Errorf("ECHO message = %q, want %q", response.Message, request.Message)
		}
	}
	return nil
}

func dial() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", address, err)
	}
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	return conn, nil
}

func writeRawFrame(w io.Writer, payload []byte) error {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
