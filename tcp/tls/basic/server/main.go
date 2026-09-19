package main

import (
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
)

func main() {
	cert, err := tls.LoadX509KeyPair(
		"../certs/server.crt",
		"../certs/server.key",
	)
	if err != nil {
		log.Fatalf("failed to load certificate: %v", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	listener, err := net.Listen("tcp", ":8443")
	if err != nil {
		log.Fatalf("failed to start TCP listener: %v", err)
	}
	defer listener.Close()

	log.Println("TLS server listening on :8443")

	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			log.Printf("failed to accept connection: %v", err)
			continue
		}

		go handleConnection(tcpConn, config)
	}
}

func handleConnection(tcpConn net.Conn, config *tls.Config) {
	tlsConn := tls.Server(tcpConn, config)
	defer tlsConn.Close()

	if err := tlsConn.Handshake(); err != nil {
		log.Printf("TLS handshake failed: %v", err)
		return
	}

	state := tlsConn.ConnectionState()

	log.Printf(
		"TLS handshake succeeded: version=%s cipher_suite=%s",
		tlsVersionName(state.Version),
		tls.CipherSuiteName(state.CipherSuite),
	)

	log.Println("Waiting for client data or connection closure")

	buffer := make([]byte, 1024)

	n, err := tlsConn.Read(buffer)

	switch {
	case err == nil:
		log.Printf("Read returned: bytes=%d", n)

	case errors.Is(err, io.EOF):
		log.Printf("TLS connection closed by peer: bytes=%d", n)

	default:
		log.Printf("TLS read failed: bytes=%d error=%v", n, err)
	}
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return "Unknown"
	}
}
