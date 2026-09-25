package dns

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const lookupTimeout = 3 * time.Second

// ErrTCPFallbackRequired indicates that a validated UDP response was truncated.
var ErrTCPFallbackRequired = errors.New("DNS response truncated: TCP fallback required")

// LookupA queries server (host:port) over UDP for matching A/IN answers.
// The overall timeout is three seconds or the context deadline, whichever is earlier.
// It does not retry or perform TCP fallback yet.
func LookupA(ctx context.Context, server string, name string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	question := Question{Name: name, Type: TypeA, Class: ClassIN}
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, fmt.Errorf("generate DNS transaction ID: %w", err)
	}
	queryID := binary.BigEndian.Uint16(idBytes[:])
	query, err := EncodeQuery(queryID, question)
	if err != nil {
		return nil, fmt.Errorf("encode query: %w", err)
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", server)
	if err != nil {
		return nil, fmt.Errorf("dial DNS server: %w", lookupIOError(ctx, err))
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set DNS deadline: %w", lookupIOError(ctx, err))
	}
	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("send DNS query: %w", lookupIOError(ctx, err))
	}
	// Maximum-sized buffer for a UDP datagram.
	buffer := make([]byte, 65535)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := conn.Read(buffer)
		if err != nil {
			return nil, fmt.Errorf("receive DNS response: %w", lookupIOError(ctx, err))
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		packet := buffer[:n]
		if len(packet) < 2 || binary.BigEndian.Uint16(packet[:2]) != queryID {
			continue
		}
		message, err := DecodeMessage(packet)
		if err != nil {
			return nil, fmt.Errorf("decode DNS response: %w", err)
		}
		if err := ValidateResponse(message, queryID, question); err != nil {
			continue
		}
		if isTruncated(message.Header.Flags) {
			return nil, ErrTCPFallbackRequired
		}
		if err := validateRCode(message); err != nil {
			return nil, err
		}
		return ExtractARecords(message, question)
	}
}

func lookupIOError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	// A socket deadline can fire just before the context's timer runs.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return context.DeadlineExceeded
		}
	}
	return err
}
