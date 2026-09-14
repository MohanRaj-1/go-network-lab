package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Each connection owns a reader, including bytes left over from earlier reads.
type requestReader struct {
	reader  io.Reader
	buffer  []byte
	readErr error
}

// readBody validates framing headers and reads the selected body format.
func readBody(r *requestReader, headers map[string][]string) ([]byte, error) {
	if encodings, exists := headers["transfer-encoding"]; exists {
		// Repeated fields describe one ordered coding list; this subset supports only chunked.
		encoding := strings.Join(encodings, ",")
		if !strings.EqualFold(encoding, "chunked") {
			return nil, fmt.Errorf("unsupported Transfer-Encoding: %q", encoding)
		}
		if _, exists := headers["content-length"]; exists {
			return nil, fmt.Errorf("cannot combine Transfer-Encoding and Content-Length")
		}
		return readChunkedBody(r)
	}
	contentLength := 0
	if values, exists := headers["content-length"]; exists {
		for i, value := range values {
			length, err := strconv.Atoi(value)
			if err != nil || length < 0 {
				return nil, fmt.Errorf("invalid Content-Length: %q", value)
			}
			if i > 0 && length != contentLength {
				return nil, fmt.Errorf("conflicting Content-Length values: %q", values)
			}
			contentLength = length
		}
	}
	return readBytes(r, contentLength)
}

// readBytes consumes exactly size bytes, leaving any following request buffered.
func readBytes(r *requestReader, size int) ([]byte, error) {
	data := make([]byte, size)
	copied := copy(data, r.buffer)
	r.buffer = r.buffer[copied:]
	if copied < size {
		if r.readErr != nil {
			if errors.Is(r.readErr, io.EOF) {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, r.readErr
		}
		if _, err := io.ReadFull(r.reader, data[copied:]); err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("read body: %w", err)
		}
	}
	return data, nil
}

func readChunkLine(r *requestReader) (string, error) {
	temp := make([]byte, 1024)
	for {
		if end := bytes.Index(r.buffer, []byte("\r\n")); end >= 0 {
			line := string(r.buffer[:end])
			r.buffer = r.buffer[end+2:]
			return line, nil
		}
		if r.readErr != nil {
			if errors.Is(r.readErr, io.EOF) {
				return "", io.ErrUnexpectedEOF
			}
			return "", r.readErr
		}
		n, err := r.reader.Read(temp)
		r.buffer = append(r.buffer, temp[:n]...)
		r.readErr = err
	}
}

func readChunkedBody(r *requestReader) ([]byte, error) {
	var body []byte
	for {
		line, err := readChunkLine(r)
		if err != nil {
			return nil, fmt.Errorf("read chunk size: %w", err)
		}
		// Only hexadecimal digits are supported; extensions are deliberately excluded.
		if line == "" || strings.Trim(line, "0123456789abcdefABCDEF") != "" {
			return nil, fmt.Errorf("invalid chunk size: %q", line)
		}
		size, err := strconv.ParseUint(line, 16, strconv.IntSize-1)
		if err != nil || size > uint64(int(^uint(0)>>1)-len(body)) {
			return nil, fmt.Errorf("chunk size too large: %q", line)
		}
		chunk, err := readBytes(r, int(size))
		if err != nil {
			return nil, err
		}
		ending, err := readBytes(r, 2)
		if err != nil {
			return nil, err
		}
		if string(ending) != "\r\n" {
			return nil, fmt.Errorf("expected CRLF after chunk")
		}
		if size == 0 {
			return body, nil
		}
		body = append(body, chunk...)
	}
}
