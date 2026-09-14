package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadRequestPreservesPipeline(t *testing.T) {
	r := &requestReader{reader: strings.NewReader("POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 5\r\n\r\nhelloGET /unknown HTTP/1.1\r\nHost: localhost\r\n\r\n")}
	first, err := readRequest(r)
	if err != nil || string(first.Body) != "hello" {
		t.Fatalf("first: %+v, %v", first, err)
	}
	second, err := readRequest(r)
	if err != nil || second.Target != "/unknown" {
		t.Fatalf("second: %+v, %v", second, err)
	}
	if _, err := readRequest(r); err != io.EOF {
		t.Fatalf("end: %v", err)
	}
}

func TestTruncatedRequest(t *testing.T) {
	for _, raw := range []string{"GET / HTTP/1.1\r\nHost: localhost\r\n", "POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 5\r\n\r\nhi"} {
		_, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("expected unexpected EOF, got %v", err)
		}
	}
}

func TestConcurrentPersistentConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()
	dial := func() net.Conn {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		return conn
	}
	stalled := dial()
	if _, err := io.WriteString(stalled, "GET /"); err != nil {
		t.Fatal(err)
	}
	conn := dial()
	reader := bufio.NewReader(conn)
	check := func(status, body string) {
		t.Helper()
		line, err := reader.ReadString('\n')
		if err != nil || line != "HTTP/1.1 "+status+"\r\n" {
			t.Fatalf("status %q, %v", line, err)
		}
		for {
			line, err = reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\r\n" {
				break
			}
		}
		got := make([]byte, len(body))
		if _, err := io.ReadFull(reader, got); err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Fatalf("body %q", got)
		}
	}
	// Two requests arrive together; both must produce responses in order.
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\nGET /unknown HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	check("200 OK", "Hello World!")
	check("404 Not Found", "Not Found")
	// Reuse the same connection, delivering the body in separate writes.
	if _, err := io.WriteString(conn, "POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 13\r\nConnection: close\r\n\r\nhello"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, " network"); err != nil {
		t.Fatal(err)
	}
	check("200 OK", "hello network")
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("expected close, got %v", err)
	}
}

func TestHTTPStatusResponses(t *testing.T) {
	cases := []struct{ name, raw, status, body string }{
		{"root", "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", "200 OK", "Hello World!"},
		{"echo", "POST /echo HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Length: 5\r\n\r\nhello", "200 OK", "hello"},
		{"unknown", "GET /unknown HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", "404 Not Found", "Not Found"},
		{"unsupported method", "PUT / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", "405 Method Not Allowed", "Method Not Allowed"},
		{"wrong method for echo", "GET /echo HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", "405 Method Not Allowed", "Method Not Allowed"},
		{"wrong method for root", "POST / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", "405 Method Not Allowed", "Method Not Allowed"},
		{"unknown path wins", "PUT /unknown HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", "404 Not Found", "Not Found"},
		{"unsupported version", "GET / HTTP/2\r\n\r\n", "400 Bad Request", "Bad Request"},
		{"old version", "GET / HTTP/1.0\r\n\r\n", "400 Bad Request", "Bad Request"},
		{"malformed line", "INVALID\r\n\r\n", "400 Bad Request", "Bad Request"},
		{"empty target", "GET  HTTP/1.1\r\nHost: localhost\r\n\r\n", "400 Bad Request", "Bad Request"},
		{"bad header", "GET / HTTP/1.1\r\nHost: localhost\r\nHost localhost\r\n\r\n", "400 Bad Request", "Bad Request"},
		{"bad length", "POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: nope\r\n\r\n", "400 Bad Request", "Bad Request"},
		{"negative length", "POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: -1\r\n\r\n", "400 Bad Request", "Bad Request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			client.SetDeadline(time.Now().Add(5 * time.Second))
			go handleConnection(server)
			if _, err := io.WriteString(client, tc.raw); err != nil {
				t.Fatal(err)
			}
			response, err := io.ReadAll(client)
			if err != nil {
				t.Fatal(err)
			}
			expected := "HTTP/1.1 " + tc.status + "\r\nContent-Length: " + strconv.Itoa(len(tc.body)) + "\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n" + tc.body
			if string(response) != expected {
				t.Fatalf("got %q; want %q", response, expected)
			}
		})
	}
}

// Restrict each Read to one byte to force splits within sizes, data, and CRLFs.
type bytewiseReader struct{ io.Reader }

func (r bytewiseReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

func TestChunkedPipeline(t *testing.T) {
	for _, fragmented := range []bool{false, true} {
		raw := "POST /echo HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n8\r\n network\r\n0\r\n\r\nGET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
		var source io.Reader = strings.NewReader(raw)
		if fragmented {
			source = bytewiseReader{source}
		}
		r := &requestReader{reader: source}
		request, err := readRequest(r)
		if err != nil || string(request.Body) != "hello network" {
			t.Fatalf("fragmented=%v: body %q, err %v", fragmented, request.Body, err)
		}
		next, err := readRequest(r)
		if err != nil || next.Method != "GET" || next.Target != "/" {
			t.Fatalf("next request: %+v, %v", next, err)
		}
	}
}

func TestChunkedBody(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"0\r\n\r\n", ""},
		{"A\r\n0123456789\r\n10\r\n0123456789abcdef\r\n0\r\n\r\n", "01234567890123456789abcdef"},
	} {
		body, err := readChunkedBody(&requestReader{reader: strings.NewReader(tc.raw)})
		if err != nil || string(body) != tc.want {
			t.Fatalf("body %q, err %v", body, err)
		}
	}
	for _, raw := range []string{
		"Z\r\n", "-1\r\n", "\r\n", "5;ext=yes\r\n", "1\r\nx!!", "0\r\n!!",
		"5\r\nhi", "5", "0\r\n", "1\r\nx\r\n", "FFFFFFFFFFFFFFFFFFFFFFFF\r\n",
	} {
		if _, err := readChunkedBody(&requestReader{reader: strings.NewReader(raw)}); err == nil {
			t.Fatalf("accepted invalid chunks %q", raw)
		}
	}
	for _, headers := range []string{"Transfer-Encoding: gzip", "Transfer-Encoding: chunked\r\nContent-Length: 5"} {
		raw := "POST /echo HTTP/1.1\r\nHost: localhost\r\n" + headers + "\r\n\r\n0\r\n\r\n"
		if _, err := readRequest(&requestReader{reader: strings.NewReader(raw)}); err == nil {
			t.Fatalf("accepted headers %q", headers)
		}
	}
}

func TestRepeatedHeaders(t *testing.T) {
	raw := "POST /echo HTTP/1.1\r\nHost: localhost\r\nAccept: text/html\r\nACCEPT: application/json\r\nX-Test: MiXeD Value\r\nContent-Length: 5\r\ncontent-length: 5\r\nConnection: keep-alive\r\nConnection: upgrade, CLOSE\r\n\r\nhello"
	request, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"accept":         {"text/html", "application/json"},
		"x-test":         {"MiXeD Value"},
		"content-length": {"5", "5"},
		"connection":     {"keep-alive", "upgrade, CLOSE"},
	} {
		got := request.Headers[name]
		if len(got) != len(want) {
			t.Fatalf("%s: %q", name, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: %q", name, got)
			}
		}
	}
	if string(request.Body) != "hello" {
		t.Fatalf("body %q", request.Body)
	}
	if !shouldClose(request) {
		t.Fatal("missed close in repeated Connection field")
	}
}

func TestRepeatedFramingHeaderCallers(t *testing.T) {
	for _, headers := range []string{
		"Content-Length: 5\r\nContent-Length: 10",
		"Content-Length: nope\r\nContent-Length: 5",
		"Transfer-Encoding: gzip\r\nTransfer-Encoding: chunked",
		"Transfer-Encoding: chunked\r\nTransfer-Encoding: gzip",
		"Transfer-Encoding: chunked\r\nTransfer-Encoding: chunked",
	} {
		raw := "POST /echo HTTP/1.1\r\nHost: localhost\r\n" + headers + "\r\n\r\nhello"
		request, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
		if err == nil {
			t.Fatalf("accepted ambiguous or unsupported framing: %q", headers)
		}
		if len(request.Headers["content-length"]) != 2 && len(request.Headers["transfer-encoding"]) != 2 {
			t.Fatalf("duplicate evidence lost: %v", request.Headers)
		}
	}
}

func TestHostSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, headers string
		valid         bool
	}{
		{"one", "Host: example.com\r\n", true},
		{"missing", "", false},
		{"empty", "Host:\r\n", false},
		{"whitespace", "Host: \t \r\n", false},
		{"identical duplicates", "Host: example.com\r\nhost: example.com\r\n", false},
		{"different duplicates", "Host: example.com\r\nHost: api.example.com\r\n", false},
		{"lowercase", "host: example.com\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := "GET / HTTP/1.1\r\n" + tc.headers + "\r\n"
			_, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestInvalidHostDoesNotConsumeBody(t *testing.T) {
	headers := "POST /echo HTTP/1.1\r\nContent-Length: 5\r\nHost: \r\n\r\n"
	t.Run("body still in stream", func(t *testing.T) {
		body := strings.NewReader("hello")
		// MultiReader returns the headers first; another Read is needed for the body.
		r := &requestReader{reader: io.MultiReader(strings.NewReader(headers), body)}
		request, err := readRequest(r)
		if err == nil || !strings.Contains(err.Error(), "Host") {
			t.Fatalf("expected Host error, got %v", err)
		}
		if body.Len() != 5 || len(request.Body) != 0 {
			t.Fatal("body consumed before semantic validation")
		}
	})
	t.Run("body already buffered", func(t *testing.T) {
		r := &requestReader{reader: strings.NewReader(headers + "hello")}
		request, err := readRequest(r)
		if err == nil || !strings.Contains(err.Error(), "Host") {
			t.Fatalf("expected Host error, got %v", err)
		}
		if string(r.buffer) != "hello" || len(request.Body) != 0 {
			t.Fatalf("body consumed: buffer=%q, body=%q", r.buffer, request.Body)
		}
	})
}

// Fail if parsing attempts to read beyond the supplied header budget.
type headerBudgetReader struct {
	t    *testing.T
	data string
}

func (r *headerBudgetReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		r.t.Fatal("Read called after header budget exhausted")
	}
	if len(p) > len(r.data) {
		r.t.Fatalf("Read requested %d bytes with only %d allowed", len(p), len(r.data))
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func headersWithSize(size int) string {
	prefix := "GET / HTTP/1.1\r\nHost: localhost\r\nX-A: "
	middle := "\r\nX-B: "
	suffix := "\r\n\r\n"
	padding := size - len(prefix) - len(middle) - len(suffix)
	return prefix + strings.Repeat("a", padding/2) + middle + strings.Repeat("b", padding-padding/2) + suffix
}

func TestHeaderLimits(t *testing.T) {
	t.Run("terminator exactly at byte limit", func(t *testing.T) {
		raw := headersWithSize(MaxHeaderBytes)
		// Start at 8190 bytes, so the next Read must request exactly two bytes.
		r := &requestReader{buffer: []byte(raw[:MaxHeaderBytes-2]), reader: &headerBudgetReader{t: t, data: raw[MaxHeaderBytes-2:]}}
		if _, err := readRequest(r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("exceeds byte limit", func(t *testing.T) {
		r := &requestReader{reader: strings.NewReader(headersWithSize(MaxHeaderBytes + 1))}
		if _, err := readRequest(r); err == nil {
			t.Fatal("accepted oversized headers")
		}
		if len(r.buffer) > MaxHeaderBytes {
			t.Fatalf("buffer grew to %d", len(r.buffer))
		}
	})
	t.Run("prebuffered oversized headers", func(t *testing.T) {
		r := &requestReader{buffer: []byte(headersWithSize(MaxHeaderBytes + 1))}
		if _, err := readRequest(r); err == nil {
			t.Fatal("accepted oversized buffered headers")
		}
	})
	t.Run("full buffer without terminator", func(t *testing.T) {
		raw := strings.TrimSuffix(headersWithSize(MaxHeaderBytes), "\r\n\r\n") + "xxxx"
		r := &requestReader{buffer: []byte(raw[:MaxHeaderBytes-1]), reader: &headerBudgetReader{t: t, data: raw[MaxHeaderBytes-1:]}}
		if _, err := readRequest(r); err == nil {
			t.Fatal("accepted unterminated headers")
		}
		if len(r.buffer) != MaxHeaderBytes {
			t.Fatalf("buffer size %d", len(r.buffer))
		}
	})
	for _, size := range []int{MaxHeaderLine, MaxHeaderLine + 1} {
		t.Run("line size "+strconv.Itoa(size), func(t *testing.T) {
			raw := "GET / HTTP/1.1\r\nHost: localhost\r\nX: " + strings.Repeat("a", size-3) + "\r\n\r\n"
			_, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
			if (err == nil) != (size == MaxHeaderLine) {
				t.Fatalf("size %d: %v", size, err)
			}
		})
	}
	for _, duplicate := range []bool{false, true} {
		for _, count := range []int{MaxHeaderCount, MaxHeaderCount + 1} {
			raw := "GET / HTTP/1.1\r\nHost: localhost\r\n"
			for i := 1; i < count; i++ {
				name := "X-Test"
				if !duplicate {
					name += strconv.Itoa(i)
				}
				raw += name + ": a\r\n"
			}
			_, err := readRequest(&requestReader{reader: strings.NewReader(raw + "\r\n")})
			if (err == nil) != (count == MaxHeaderCount) {
				t.Fatalf("duplicate=%v count=%d: %v", duplicate, count, err)
			}
		}
	}
	t.Run("buffered body and next request excluded", func(t *testing.T) {
		next := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
		body := strings.Repeat("b", MaxHeaderBytes+1)
		raw := "POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body + next
		r := &requestReader{buffer: []byte(raw)}
		request, err := readRequest(r)
		if err != nil || string(request.Body) != body {
			t.Fatalf("body read: %v", err)
		}
		if _, err := readRequest(r); err != nil {
			t.Fatal(err)
		}
	})
}

func TestIsValidHeaderName(t *testing.T) {
	for _, name := range []string{"Content-Type", "X-Test2", "X_Request", "X.Test", "Host", "X-Request-ID", "!#$%&'*+-.^_`|~", "0123456789"} {
		if !isValidHeaderName(name) {
			t.Errorf("rejected valid name %q", name)
		}
	}
	for _, name := range []string{"Host Name", "Host@", "\u00e9-Test", "\u4e2d-Test", "", "Host:", "Host\tName", " Host", "Host ", "X\x00", "X\x7f", "X\r", "X\n"} {
		if isValidHeaderName(name) {
			t.Errorf("accepted invalid name %q", name)
		}
	}
}

func TestHeaderNameParsing(t *testing.T) {
	for _, line := range []string{": value", "Host Name: example.com", "Host@: example.com", "Host\tName: example.com", "Host : example.com", " Host: example.com", "\u00e9-Test: value", "\u4e2d-Test: value"} {
		raw := "GET / HTTP/1.1\r\nHost: localhost\r\n" + line + "\r\n\r\n"
		if _, err := readRequest(&requestReader{reader: strings.NewReader(raw)}); err == nil {
			t.Errorf("accepted invalid field %q", line)
		}
	}
	raw := "GET / HTTP/1.1\r\nHost: example.com\r\nContent-Type: text/plain\r\nX-Test2: hello\r\nX-Request-ID: 123\r\nX-Test: :value\r\n\r\n"
	request, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if request.Headers["x-test"][0] != ":value" {
		t.Fatal("colon in value was not preserved")
	}
}

func TestIsValidHeaderValue(t *testing.T) {
	// Check all 256 byte values, including non-UTF-8 bytes.
	for b := 0; b <= 255; b++ {
		value := string([]byte{byte(b)})
		want := b == 0x09 || b >= 0x20 && b != 0x7f
		if got := isValidHeaderValue(value); got != want {
			t.Errorf("byte 0x%02x: got %v, want %v", b, got, want)
		}
	}
	for _, value := range []string{"", "hello world", "hello:world", "123", "!@#$%^&*()", "hello\tworld", "\u00e9\u4e2d"} {
		if !isValidHeaderValue(value) {
			t.Errorf("rejected %q", value)
		}
	}
}

func TestHeaderValueParsing(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"hello world", "hello world"},
		{"hello:world", "hello:world"},
		{"123", "123"},
		{"!@#$%^&*()", "!@#$%^&*()"},
		{"", ""},
		{" \t ", ""},
		{" \thello\tworld \t", "hello\tworld"},
		{" \u00a0hello\u00a0 ", "\u00a0hello\u00a0"},
		{"\u00e9\u4e2d", "\u00e9\u4e2d"},
		{"\x80\xff", "\x80\xff"},
	} {
		raw := "GET / HTTP/1.1\r\nHost: localhost\r\nX-Test:" + tc.raw + "\r\n\r\n"
		request, err := readRequest(&requestReader{reader: strings.NewReader(raw)})
		if err != nil {
			t.Fatalf("value %q: %v", tc.raw, err)
		}
		got := request.Headers["x-test"]
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("value %q: stored %q, want %q", tc.raw, got, tc.want)
		}
	}
	// Leading/trailing controls must be rejected before any whitespace trimming.
	for b := 0; b <= 0x7f; b++ {
		if b == 0x09 || b >= 0x20 && b != 0x7f {
			continue
		}
		control := string([]byte{byte(b)})
		for _, value := range []string{control + "hello", "hello" + control, "hello" + control + "world"} {
			raw := "GET / HTTP/1.1\r\nHost: localhost\r\nX-Test:" + value + "\r\n\r\n"
			if _, err := readRequest(&requestReader{reader: strings.NewReader(raw)}); err == nil {
				t.Errorf("accepted forbidden control in %q", value)
			}
		}
	}
}
