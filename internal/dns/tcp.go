package dns

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// exchangeTCP sends one length-prefixed query and returns one complete payload.
// It uses the caller's deadline without starting a new timeout.
func exchangeTCP(ctx context.Context, server string, query []byte) ([]byte, error) {
	if len(query) > 65535 {
		return nil, fmt.Errorf("DNS TCP query exceeds 65535 bytes: %d", len(query))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, fmt.Errorf("dial DNS TCP server: %w", lookupIOError(ctx, err))
	}
	defer conn.Close()
	return exchangeTCPConn(ctx, conn, query)
}

// exchangeTCPConn performs the framed I/O on an established connection.
// It checks query size independently so direct callers cannot truncate the length.
func exchangeTCPConn(ctx context.Context, conn net.Conn, query []byte) ([]byte, error) {
	if len(query) > 65535 {
		return nil, fmt.Errorf("DNS TCP query exceeds 65535 bytes: %d", len(query))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set DNS TCP deadline: %w", lookupIOError(ctx, err))
		}
	}
	frame := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(query)))
	copy(frame[2:], query)
	if err := writeFull(conn, frame); err != nil {
		return nil, fmt.Errorf("write DNS TCP query: %w", lookupIOError(ctx, err))
	}
	var prefix [2]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		return nil, fmt.Errorf("read DNS TCP length: %w", lookupIOError(ctx, err))
	}
	length := int(binary.BigEndian.Uint16(prefix[:]))
	if length < 12 {
		return nil, fmt.Errorf("invalid DNS TCP response length: %d, minimum 12", length)
	}
	response := make([]byte, length)
	if _, err := io.ReadFull(conn, response); err != nil {
		return nil, fmt.Errorf("read DNS TCP response: %w", lookupIOError(ctx, err))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return response, nil
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
