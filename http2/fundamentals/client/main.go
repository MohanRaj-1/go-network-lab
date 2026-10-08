package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

func main() {
	transport := &http.Transport{
		MaxConnsPerHost:     1,
		MaxIdleConnsPerHost: 1,
		DisableKeepAlives:   false,

		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},

		ForceAttemptHTTP2: true,
	}

	client := &http.Client{
		Transport: transport,
	}

	done := make(chan struct{}, 2)

	go func() {
		measure(client, "slow")
		done <- struct{}{}
	}()

	time.Sleep(100 * time.Millisecond)

	go func() {
		measure(client, "fast")
		done <- struct{}{}
	}()

	<-done
	<-done
}

func measure(client *http.Client, name string) {
	start := time.Now()

	fmt.Printf("requesting https://localhost:9500/%s\n", name)
	resp, err := client.Get("https://localhost:9500/" + name)
	if err != nil {
		log.Fatalf("%s request failed: %v", name, err)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	fmt.Printf(
		"%s: protocol=%s status=%s duration=%s\n",
		name,
		resp.Proto,
		resp.Status,
		time.Since(start),
	)
}
