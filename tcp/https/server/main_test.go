package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPSConnection(t *testing.T) {
	certPath := filepath.Join(
		"..",
		"..",
		"..",
		"tcp",
		"tls",
		"basic",
		"certs",
		"server.crt",
	)

	keyPath := filepath.Join(
		"..",
		"..",
		"..",
		"tcp",
		"tls",
		"basic",
		"certs",
		"server.key",
	)

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("load TLS certificate: %v", err)
	}

	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	clientTLSConfig := &tls.Config{
		RootCAs:    certificatePool(t, certPath),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	serverConn, clientConn := net.Pipe()

	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		handleConnection(serverConn, serverTLSConfig)
	}()

	tlsClient := tls.Client(clientConn, clientTLSConfig)
	defer tlsClient.Close()

	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("TLS client handshake: %v", err)
	}

	request := strings.Join([]string{
		"GET / HTTP/1.1",
		"Host: localhost",
		"Connection: close",
		"",
		"",
	}, "\r\n")

	if _, err := tlsClient.Write([]byte(request)); err != nil {
		t.Fatalf("write HTTP request: %v", err)
	}

	response, err := io.ReadAll(tlsClient)
	if err != nil {
		t.Fatalf("read HTTP response: %v", err)
	}

	responseText := string(response)

	if !strings.Contains(responseText, "HTTP/1.1 200 OK") {
		t.Fatalf("expected 200 OK response, got:\n%s", responseText)
	}

	if !strings.Contains(responseText, "Hello World!") {
		t.Fatalf("expected Hello World! in response, got:\n%s", responseText)
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not finish within timeout")
	}
}

func certificatePool(t *testing.T, certPath string) *x509.CertPool {
	t.Helper()

	certificatePEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read certificate: %v", err)
	}

	pool := x509.NewCertPool()

	if !pool.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("failed to add certificate to certificate pool")
	}

	return pool
}

func TestHTTPSPostEcho(t *testing.T) {
	certPath := filepath.Join(
		"..",
		"..",
		"..",
		"tcp",
		"tls",
		"basic",
		"certs",
		"server.crt",
	)

	keyPath := filepath.Join(
		"..",
		"..",
		"..",
		"tcp",
		"tls",
		"basic",
		"certs",
		"server.key",
	)

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("load TLS certificate: %v", err)
	}

	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	clientTLSConfig := &tls.Config{
		RootCAs:    certificatePool(t, certPath),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	}

	serverConn, clientConn := net.Pipe()

	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		handleConnection(serverConn, serverTLSConfig)
	}()

	tlsClient := tls.Client(clientConn, clientTLSConfig)
	defer tlsClient.Close()

	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("TLS client handshake: %v", err)
	}

	body := "Hello from HTTPS integration test"

	request := strings.Join([]string{
		"POST /echo HTTP/1.1",
		"Host: localhost",
		"Content-Length: " + fmt.Sprint(len(body)),
		"Content-Type: text/plain",
		"Connection: close",
		"",
		body,
	}, "\r\n")

	if _, err := tlsClient.Write([]byte(request)); err != nil {
		t.Fatalf("write HTTP request: %v", err)
	}

	response, err := io.ReadAll(tlsClient)
	if err != nil {
		t.Fatalf("read HTTP response: %v", err)
	}

	responseText := string(response)

	if !strings.Contains(responseText, "HTTP/1.1 200 OK") {
		t.Fatalf("expected 200 OK response, got:\n%s", responseText)
	}

	if !strings.Contains(responseText, body) {
		t.Fatalf("expected echoed body %q in response, got:\n%s", body, responseText)
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not finish within timeout")
	}
}
