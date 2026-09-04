// Package protocol implements the application protocol used by go-network-lab.
// Each message is JSON preceded by a four-byte, big-endian payload length.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	Version      = 1
	TypePing     = "PING"
	TypePong     = "PONG"
	TypeEcho     = "ECHO"
	TypeTime     = "TIME"
	TypeError    = "ERROR"
	MaxFrameSize = 1 << 20 // 1 MiB
)

var ErrFrameTooLarge = errors.New("protocol: frame too large")

type Request struct {
	Version int    `json:"version"`
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

type Response struct {
	Version int    `json:"version"`
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Value   string `json:"value,omitempty"`
}

// Write encodes v as JSON and writes one complete frame.
func Write(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if len(payload) > MaxFrameSize {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, len(payload))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeAll(w, header[:]); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}
	if err := writeAll(w, payload); err != nil {
		return fmt.Errorf("write frame payload: %w", err)
	}
	return nil
}

// Read reads one complete frame and decodes its JSON payload into v.
func Read(r io.Reader, v any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return fmt.Errorf("read frame length: %w", err)
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > MaxFrameSize {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return fmt.Errorf("read frame payload: %w", err)
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("decode message: %w", err)
	}
	return nil
}

func (r Request) Validate() error {
	if r.Version != Version {
		return fmt.Errorf("unsupported protocol version %d", r.Version)
	}
	if r.ID <= 0 {
		return errors.New("id must be positive")
	}
	switch r.Type {
	case TypePing, TypeTime:
		return nil
	case TypeEcho:
		if r.Message == "" {
			return errors.New("ECHO requires a message")
		}
		return nil
	default:
		return fmt.Errorf("unknown request type %q", r.Type)
	}
}

func ErrorResponse(id int, err error) Response {
	return Response{Version: Version, ID: id, Type: TypeError, Message: err.Error()}
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
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
