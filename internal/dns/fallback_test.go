package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// Serve UDP and TCP on the same port and compare the queries across transports.
func fallbackTestServer(t *testing.T, udpDelay time.Duration, handle func(net.Conn, []byte)) string {
	t.Helper()
	queries := make(chan []byte, 1)
	server := tcpTestServer(t, func(conn net.Conn) {
		query := readTCPTestQuery(t, conn)
		select {
		case original := <-queries:
			if !bytes.Equal(query, original) {
				t.Errorf("TCP query differs from UDP query: %x vs %x", query, original)
			}
		case <-time.After(time.Second):
			t.Error("missing UDP query")
			return
		}
		handle(conn, query)
	})
	addr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { udp.Close(); <-done })
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		n, peer, err := udp.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		query := append([]byte(nil), buffer[:n]...)
		queries <- query
		time.Sleep(udpDelay)
		// RCODE and this address must be discarded in favor of the TCP response.
		_, err = udp.WriteToUDP(lookupTestResponse(query, FlagQR|FlagTC|3, []byte{9, 9, 9, 9}), peer)
		if err != nil {
			t.Error(err)
		}
	}()
	return server
}

func sendFallbackResponse(t *testing.T, conn net.Conn, response []byte) {
	t.Helper()
	frame := make([]byte, 2+len(response))
	binary.BigEndian.PutUint16(frame, uint16(len(response)))
	copy(frame[2:], response)
	if err := writeFull(conn, frame); err != nil {
		t.Error(err)
	}
}

func TestLookupATCPFallback(t *testing.T) {
	server := fallbackTestServer(t, 0, func(conn net.Conn, query []byte) {
		sendFallbackResponse(t, conn, lookupTestResponse(query, FlagQR, []byte{1, 2, 3, 4}))
	})
	got, err := LookupA(context.Background(), server, "example.com")
	if err != nil || len(got) != 1 || got[0].String() != "1.2.3.4" {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestLookupATCPFallbackErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
		text   string
		code   uint16
	}{
		{"wrong ID", func(q []byte) []byte { r := lookupTestResponse(q, FlagQR); r[0] ^= 1; return r }, "transaction ID", 0},
		{"query QR", func(q []byte) []byte { return lookupTestResponse(q, 0) }, "QR", 0},
		{"wrong question", func(q []byte) []byte { r := lookupTestResponse(q, FlagQR); r[13] = 'z'; return r }, "name mismatch", 0},
		{"malformed message", func(q []byte) []byte { return lookupTestResponse(q, FlagQR)[:12] }, "decode DNS TCP", 0},
		{"still truncated", func(q []byte) []byte { return lookupTestResponse(q, FlagQR|FlagTC|3) }, "still truncated", 0},
		{"NXDOMAIN", func(q []byte) []byte { return lookupTestResponse(q, FlagQR|3) }, "", 3},
		{"bad A data", func(q []byte) []byte { return lookupTestResponse(q, FlagQR, []byte{1}) }, "invalid A record", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := fallbackTestServer(t, 0, func(conn net.Conn, query []byte) { sendFallbackResponse(t, conn, tc.change(query)) })
			got, err := LookupA(context.Background(), server, "example.com")
			if got != nil || err == nil || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("got (%v, %v)", got, err)
			}
			if tc.code != 0 {
				var dnsErr *DNSResponseError
				if !errors.As(err, &dnsErr) || dnsErr.Code != tc.code {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestLookupATCPFallbackCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := fallbackTestServer(t, 0, func(conn net.Conn, query []byte) {
		cancel()
		var b [1]byte
		conn.Read(b[:])
	})
	got, err := LookupA(ctx, server, "example.com")
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestLookupATCPFallbackSharedTimeout(t *testing.T) {
	// Spend part of LookupA's own budget on UDP; TCP must not get a fresh timeout.
	server := fallbackTestServer(t, time.Second, func(conn net.Conn, query []byte) {
		conn.SetDeadline(time.Now().Add(4 * time.Second))
		var b [1]byte
		conn.Read(b[:])
	})
	start := time.Now()
	got, err := LookupA(context.Background(), server, "example.com")
	elapsed := time.Since(start)
	if got != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got (%v, %v)", got, err)
	}
	if elapsed > lookupTimeout+750*time.Millisecond {
		t.Fatalf("lookup deadline was extended: %v", elapsed)
	}
}
