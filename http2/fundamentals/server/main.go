package main

import (
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

	addr := ":9500"

	fmt.Printf("HTTPS server listening on %s\n", addr)

	certFile := "tcp/tls/basic/certs/server.crt"
	keyFile := "tcp/tls/basic/certs/server.key"

	if err := http.ListenAndServeTLS(addr, certFile, keyFile, mux); err != nil {
		log.Fatal(err)
	}
}
