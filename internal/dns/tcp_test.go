package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func tcpTestServer(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		handle(conn)
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return listener.Addr().String()
}

func readTCPTestQuery(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	var prefix [2]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		t.Error(err)
		return nil
	}
	query := make([]byte, int(binary.BigEndian.Uint16(prefix[:])))
	if _, err := io.ReadFull(conn, query); err != nil {
		t.Error(err)
		return nil
	}
	return query
}

func TestExchangeTCPFragmentedResponse(t *testing.T) {
	query := []byte{0x12, 0x34, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	response := bytes.Repeat([]byte{0xab}, 61) // Opaque payload: no DNS decoding here.
	server := tcpTestServer(t, func(conn net.Conn) {
		if got := readTCPTestQuery(t, conn); !bytes.Equal(got, query) {
			t.Errorf("query changed: %x", got)
		}
		for _, part := range [][]byte{{0}, {61}, response[:3], response[3:20], response[20:]} {
			if err := writeFull(conn, part); err != nil {
				t.Error(err)
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := exchangeTCP(ctx, server, query)
	if err != nil || !bytes.Equal(got, response) {
		t.Fatalf("got (%x, %v)", got, err)
	}
}

func TestExchangeTCPMinimumResponse(t *testing.T) {
	response := make([]byte, 12)
	server := tcpTestServer(t, func(conn net.Conn) {
		readTCPTestQuery(t, conn)
		if err := writeFull(conn, append([]byte{0, 12}, response...)); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := exchangeTCP(ctx, server, make([]byte, 12))
	if err != nil || !bytes.Equal(got, response) {
		t.Fatalf("got (%x, %v)", got, err)
	}
}

func TestExchangeTCPIncompleteFrames(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire []byte
		want error
		text string
	}{
		{"empty prefix", nil, io.EOF, "length"},
		{"partial prefix", []byte{0}, io.ErrUnexpectedEOF, "length"},
		{"empty body", []byte{0, 12}, io.EOF, "response"},
		{"partial body", []byte{0, 12, 1, 2}, io.ErrUnexpectedEOF, "response"},
		{"zero length", []byte{0, 0}, nil, "minimum 12"},
		{"short length", []byte{0, 11}, nil, "minimum 12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := tcpTestServer(t, func(conn net.Conn) {
				readTCPTestQuery(t, conn)
				if err := writeFull(conn, tc.wire); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := exchangeTCP(ctx, server, make([]byte, 12))
			if got != nil || err == nil || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("got (%x, %v)", got, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestExchangeTCPQuerySize(t *testing.T) {
	if got, err := exchangeTCP(context.Background(), "invalid", make([]byte, 65536)); got != nil || err == nil || !strings.Contains(err.Error(), "65535") {
		t.Fatalf("got (%v, %v)", got, err)
	}
	query := bytes.Repeat([]byte{42}, 65535)
	server := tcpTestServer(t, func(conn net.Conn) {
		if got := readTCPTestQuery(t, conn); !bytes.Equal(got, query) {
			t.Errorf("maximum query changed: length %d", len(got))
		}
		writeFull(conn, append([]byte{0, 12}, make([]byte, 12)...))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := exchangeTCP(ctx, server, query); err != nil || len(got) != 12 {
		t.Fatalf("length %d, error %v", len(got), err)
	}
}

func TestExchangeTCPCancelRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := tcpTestServer(t, func(conn net.Conn) {
		readTCPTestQuery(t, conn)
		cancel()
		var b [1]byte
		conn.Read(b[:]) // Wait for client closure, not a server-initiated EOF.
	})
	got, err := exchangeTCP(ctx, server, make([]byte, 12))
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestExchangeTCPReadDeadline(t *testing.T) {
	for _, body := range []bool{false, true} {
		name := "prefix"
		if body {
			name = "body"
		}
		t.Run(name, func(t *testing.T) {
			server := tcpTestServer(t, func(conn net.Conn) {
				readTCPTestQuery(t, conn)
				if body {
					writeFull(conn, []byte{0, 12, 1})
				}
				var b [1]byte
				conn.Read(b[:])
			})
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			got, err := exchangeTCP(ctx, server, make([]byte, 12))
			if got != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("got (%v, %v)", got, err)
			}
		})
	}
}

func TestExchangeTCPWriteDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close() // No reader: writing must block until the existing deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got, err := exchangeTCPConn(ctx, client, make([]byte, 12))
	if got != nil || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "write") {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

type shortTCPWriter struct {
	bytes.Buffer
	limit int
}

func (w *shortTCPWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	return w.Buffer.Write(p)
}

func TestTCPWriteFull(t *testing.T) {
	w := &shortTCPWriter{limit: 2}
	data := []byte{0, 4, 1, 2, 3, 4}
	if err := writeFull(w, data); err != nil || !bytes.Equal(w.Bytes(), data) {
		t.Fatalf("got %x, error %v", w.Bytes(), err)
	}
	w.limit = 0
	if err := writeFull(w, data); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("zero progress: %v", err)
	}
}
