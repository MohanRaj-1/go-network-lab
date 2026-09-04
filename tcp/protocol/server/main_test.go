package main

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/MohanRaj-1/go-network-lab/protocol"
)

func TestDispatchMarksProtocolErrorsFatal(t *testing.T) {
	result := dispatch(protocol.Request{
		Version: protocol.Version,
		ID:      4,
		Type:    "HELLO",
	}, time.Now())

	if result.response.Type != protocol.TypeError {
		t.Fatalf("response type = %q, want %q", result.response.Type, protocol.TypeError)
	}
	if !result.closeConnection {
		t.Fatal("protocol error did not request connection close")
	}
}

func TestHandleConnectionClosesAfterProtocolError(t *testing.T) {
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleConnection(server)
		close(done)
	}()
	defer client.Close()

	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	request := protocol.Request{Version: protocol.Version, ID: 4, Type: "HELLO"}
	if err := protocol.Write(client, request); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var response protocol.Response
	if err := protocol.Read(client, &response); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.Type != protocol.TypeError {
		t.Fatalf("response type = %q, want %q", response.Type, protocol.TypeError)
	}

	var next protocol.Response
	if err := protocol.Read(client, &next); !errors.Is(err, io.EOF) {
		t.Fatalf("read after error = %v, want EOF", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleConnection did not return")
	}
}
