package main

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/MohanRaj-1/go-network-lab/internal/rawhttp"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	cert, err := tls.LoadX509KeyPair(
		"tcp/tls/basic/certs/server.crt",
		"tcp/tls/basic/certs/server.key",
	)
	if err != nil {
		return fmt.Errorf("load TLS certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	listener, err := net.Listen("tcp", ":8443")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	fmt.Println("HTTPS server listening on https://localhost:8443")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept:", err)
			continue
		}

		go handleConnection(conn, tlsConfig)
	}
}

func handleConnection(conn net.Conn, tlsConfig *tls.Config) {
	defer conn.Close()

	tlsConn := tls.Server(conn, tlsConfig)
	defer tlsConn.Close()

	if err := establishTLS(tlsConn); err != nil {
		fmt.Println("TLS handshake:", err)
		return
	}

	rawhttp.HandleConnection(tlsConn)
}

func establishTLS(tlsConn *tls.Conn) error {
	if err := tlsConn.Handshake(); err != nil {
		return err
	}

	state := tlsConn.ConnectionState()

	fmt.Printf(
		"TLS connection established: version=%s cipher=%s server_name=%s alpn=%s\n",
		tlsVersionName(state.Version),
		tls.CipherSuiteName(state.CipherSuite),
		state.ServerName,
		state.NegotiatedProtocol,
	)

	return nil
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", version)
	}
}
