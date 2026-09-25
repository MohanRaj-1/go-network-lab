package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// Each server handles one query; all traffic stays on loopback.
func lookupTestServer(t *testing.T, reply func([]byte) [][]byte) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { conn.Close(); <-done })
	go func() {
		defer close(done)
		buffer := make([]byte, 65535)
		n, peer, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		for _, packet := range reply(buffer[:n]) {
			if _, err := conn.WriteToUDP(packet, peer); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	return conn.LocalAddr().String()
}

func lookupTestResponse(query []byte, flags uint16, answers ...[]byte) []byte {
	response := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(response[2:4], flags)
	binary.BigEndian.PutUint16(response[6:8], uint16(len(answers)))
	for _, data := range answers {
		response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 111, 0, byte(len(data)))
		response = append(response, data...)
	}
	return response
}

func TestLookupA(t *testing.T) {
	server := lookupTestServer(t, func(query []byte) [][]byte {
		message, err := DecodeMessage(query)
		if err != nil {
			t.Error(err)
			return nil
		}
		if message.Header.Flags != FlagRD || len(message.Questions) != 1 || message.Questions[0] != (Question{Name: "example.com", Type: TypeA, Class: ClassIN}) {
			t.Errorf("unexpected query: %+v", message)
		}
		return [][]byte{lookupTestResponse(query, FlagQR, []byte{172, 66, 147, 243}, []byte{104, 20, 23, 154})}
	})
	got, err := LookupA(context.Background(), server, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].String() != "172.66.147.243" || got[1].String() != "104.20.23.154" {
		t.Fatalf("unexpected addresses: %v", got)
	}
}

func TestLookupAIgnoresUnrelatedPackets(t *testing.T) {
	server := lookupTestServer(t, func(query []byte) [][]byte {
		wrongID := append([]byte(nil), query[:2]...)
		wrongID[0] ^= 1 // Deliberately malformed, but unrelated.
		wrongName := lookupTestResponse(query, FlagQR)
		wrongName[13] = 'z'
		return [][]byte{{1}, wrongID, lookupTestResponse(query, 0), wrongName, lookupTestResponse(query, FlagQR, []byte{1, 2, 3, 4})}
	})
	got, err := LookupA(context.Background(), server, "example.com")
	if err != nil || len(got) != 1 || got[0].String() != "1.2.3.4" {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestLookupAResponseErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reply    func([]byte) []byte
		text     string
		code     uint16
		fallback bool
	}{
		{"malformed matching ID", func(q []byte) []byte { return q[:2] }, "decode DNS response", 0, false},
		{"NXDOMAIN", func(q []byte) []byte { return lookupTestResponse(q, FlagQR|3) }, "", 3, false},
		{"SERVFAIL", func(q []byte) []byte { return lookupTestResponse(q, FlagQR|2) }, "", 2, false},
		{"truncated before RCODE", func(q []byte) []byte { return lookupTestResponse(q, FlagQR|FlagTC|3) }, "", 0, true},
		{"malformed A", func(q []byte) []byte { return lookupTestResponse(q, FlagQR, []byte{1, 2}) }, "invalid A record", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := lookupTestServer(t, func(q []byte) [][]byte { return [][]byte{tc.reply(q)} })
			got, err := LookupA(context.Background(), server, "example.com")
			if err == nil || got != nil {
				t.Fatalf("expected no result and error, got (%v, %v)", got, err)
			}
			if tc.text != "" && !strings.Contains(err.Error(), tc.text) {
				t.Fatal(err)
			}
			if tc.fallback && !errors.Is(err, ErrTCPFallbackRequired) {
				t.Fatal(err)
			}
			if tc.code != 0 {
				var dnsErr *DNSResponseError
				if !errors.As(err, &dnsErr) || dnsErr.Code != tc.code {
					t.Fatalf("expected code %d: %v", tc.code, err)
				}
			}
		})
	}
}

func TestLookupANoAnswers(t *testing.T) {
	server := lookupTestServer(t, func(q []byte) [][]byte { return [][]byte{lookupTestResponse(q, FlagQR)} })
	got, err := LookupA(context.Background(), server, "example.com")
	if err != nil || len(got) != 0 {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestLookupACancellationDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := lookupTestServer(t, func(q []byte) [][]byte { cancel(); return nil })
	start := time.Now()
	got, err := LookupA(ctx, server, "example.com")
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not interrupt receive promptly")
	}
}

func TestLookupADeadline(t *testing.T) {
	server := lookupTestServer(t, func(q []byte) [][]byte {
		wrong := append([]byte(nil), q[:2]...)
		wrong[0] ^= 1
		return [][]byte{wrong}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := LookupA(ctx, server, "example.com")
	if !errors.Is(err, context.DeadlineExceeded) || got != nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("caller deadline was not honored")
	}
}

func TestLookupADefaultTimeout(t *testing.T) {
	server := lookupTestServer(t, func(q []byte) [][]byte { return nil })
	got, err := LookupA(context.Background(), server, "example.com")
	if !errors.Is(err, context.DeadlineExceeded) || got != nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestLookupAInvalidInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LookupA(ctx, "127.0.0.1:53", "example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if _, err := LookupA(context.Background(), "127.0.0.1:53", "bad..name"); err == nil {
		t.Fatal("accepted invalid name")
	}
	if _, err := LookupA(context.Background(), "127.0.0.1", "example.com"); err == nil {
		t.Fatal("accepted server without port")
	}
}
