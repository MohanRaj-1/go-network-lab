package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		time.Sleep(2 * time.Second)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("slow response\n"))

		log.Printf(
			"method=%s path=%s duration=%s",
			r.Method,
			r.URL.Path,
			time.Since(start),
		)
	})

	mux.HandleFunc("/fast", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fast response\n"))

		log.Printf(
			"method=%s path=%s duration=%s",
			r.Method,
			r.URL.Path,
			time.Since(start),
		)
	})

	mux.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		const (
			chunkSize  = 16 * 1024       // 16 KiB
			totalBytes = 4 * 1024 * 1024 // 4 MiB
		)

		chunk := bytes.Repeat([]byte("A"), chunkSize)

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(totalBytes))

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		for written := 0; written < totalBytes; written += chunkSize {
			n, err := w.Write(chunk)
			if err != nil {
				log.Printf("path=%s write_error=%v", r.URL.Path, err)
				return
			}
			if n != len(chunk) {
				log.Printf("path=%s short_write=%d", r.URL.Path, n)
				return
			}

			flusher.Flush()
		}

		log.Printf(
			"method=%s path=%s bytes=%d duration=%s",
			r.Method,
			r.URL.Path,
			totalBytes,
			time.Since(start),
		)
	})

	addr := ":9500"

	fmt.Printf("HTTPS server listening on %s\n", addr)

	certFile := "tcp/tls/basic/certs/server.crt"
	keyFile := "tcp/tls/basic/certs/server.key"

	if err := http.ListenAndServeTLS(addr, certFile, keyFile, mux); err != nil {
		log.Fatal(err)
	}
}
