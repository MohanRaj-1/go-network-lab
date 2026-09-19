package main

import (
	"crypto/tls"
	"crypto/x509"
	"log"
	"os"
)

func main() {
	certPEM, err := os.ReadFile("../certs/server.crt")
	if err != nil {
		log.Fatalf("failed to read server certificate: %v", err)
	}

	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(certPEM); !ok {
		log.Fatal("failed to add server certificate to trust pool")
	}

	keyLogFile, err := os.Create("tls-keys.log")
	if err != nil {
		log.Fatalf("failed to create TLS key log: %v", err)
	}
	defer keyLogFile.Close()

	config := &tls.Config{
		RootCAs:      certPool,
		ServerName:   "localhost",
		KeyLogWriter: keyLogFile,
	}

	conn, err := tls.Dial("tcp", "localhost:8443", config)
	if err != nil {
		log.Fatalf("TLS connection failed: %v", err)
	}

	if err := conn.Handshake(); err != nil {
		log.Fatalf("TLS handshake failed: %v", err)
	}

	log.Println("TLS handshake completed")

	state := conn.ConnectionState()

	log.Printf(
		"TLS connection established: version=%s cipher_suite=%s",
		tlsVersionName(state.Version),
		tls.CipherSuiteName(state.CipherSuite),
	)

	if err := conn.Close(); err != nil {
		log.Printf("TLS close failed: %v", err)
		return
	}

	log.Println("TLS connection closed gracefully")
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
