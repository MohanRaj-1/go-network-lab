package rawhttp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	MaxHeaderBytes = 8 * 1024 // Request line, headers, and final CRLF CRLF.
	MaxHeaderLine  = 4 * 1024 // Parsed line content, excluding CRLF.
	MaxHeaderCount = 100      // Field occurrences, including duplicates.
)

func readRequest(r *requestReader) (Request, error) {
	var request Request
	separator := []byte("\r\n\r\n")
	temp := make([]byte, 1024)
	end := bytes.Index(r.buffer, separator)
	for end < 0 {
		remaining := MaxHeaderBytes - len(r.buffer)
		if remaining <= 0 {
			return request, fmt.Errorf("headers exceed maximum of %d bytes", MaxHeaderBytes)
		}
		if r.readErr != nil {
			if errors.Is(r.readErr, io.EOF) && len(r.buffer) > 0 {
				return request, io.ErrUnexpectedEOF
			}
			return request, r.readErr
		}
		n, err := r.reader.Read(temp[:min(len(temp), remaining)])
		r.buffer = append(r.buffer, temp[:n]...)
		r.readErr = err // Process received bytes before handling an accompanying error.
		end = bytes.Index(r.buffer, separator)
	}
	// Buffered data may also contain a body or another request; count only headers.
	if end+len(separator) > MaxHeaderBytes {
		return request, fmt.Errorf("headers exceed maximum of %d bytes", MaxHeaderBytes)
	}
	lines := strings.Split(string(r.buffer[:end]), "\r\n")
	if len(lines)-1 > MaxHeaderCount {
		return request, fmt.Errorf("headers exceed maximum count of %d", MaxHeaderCount)
	}
	for _, line := range lines {
		if len(line) > MaxHeaderLine {
			return request, fmt.Errorf("header line exceeds maximum of %d bytes", MaxHeaderLine)
		}
	}
	parts := strings.Split(lines[0], " ")
	if len(parts) != 3 {
		return request, fmt.Errorf("invalid request line: %q", lines[0])
	}
	request = Request{Method: parts[0], Target: parts[1], Version: parts[2], Headers: make(map[string][]string)}
	if err := validateRequest(request); err != nil {
		return request, err
	}
	for _, line := range lines[1:] {
		name, value, found := strings.Cut(line, ":")
		if !found || !isValidHeaderName(name) {
			return request, fmt.Errorf("invalid header: %q", line)
		}
		name = strings.ToLower(name)
		if !isValidHeaderValue(value) {
			return request, fmt.Errorf("invalid header value for %q: %q", name, value)
		}
		request.Headers[name] = append(request.Headers[name], strings.Trim(value, " \t"))
	}
	r.buffer = r.buffer[end+len(separator):]
	if err := validateRequestSemantics(request); err != nil {
		return request, err
	}
	body, err := readBody(r, request.Headers)
	request.Body = body
	return request, err
}

// Field names are nonempty sequences of ASCII token bytes.
func isValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' {
			continue
		}
		switch b {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		}
		return false
	}
	return true
}

// Validate raw bytes before trimming so invalid controls cannot disappear.
func isValidHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b == '\t' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

// Our learning subset accepts an origin-form target and HTTP/1.1 only.
// A well-formed but unsupported method is handled by routing, not as a parse error.
func validateRequest(request Request) error {
	if request.Method == "" || strings.ContainsAny(request.Method, "()<>@,;:\\\"/[]?={} \t\r\n") {
		return fmt.Errorf("invalid method: %q", request.Method)
	}
	for _, ch := range request.Method {
		if ch < 33 || ch > 126 {
			return fmt.Errorf("invalid method: %q", request.Method)
		}
	}
	if !strings.HasPrefix(request.Target, "/") || strings.ContainsAny(request.Target, "\t\r\n") {
		return fmt.Errorf("invalid target: %q", request.Target)
	}
	if request.Version != "HTTP/1.1" {
		return fmt.Errorf("unsupported version: %q", request.Version)
	}
	return nil
}
