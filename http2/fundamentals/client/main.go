package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {

	keyLogPath := filepath.Join(os.TempDir(), "go-network-lab-tls-keys.log")

	keyLogFile, err := os.Create(keyLogPath)
	if err != nil {
		log.Fatal(err)
	}
	defer keyLogFile.Close()

	fmt.Println("TLS key log:", keyLogPath)

	transport := &http.Transport{
		MaxConnsPerHost:     1,
		MaxIdleConnsPerHost: 1,
		DisableKeepAlives:   false,

		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Local self-signed certificate experiment only.
			KeyLogWriter:       keyLogFile,
		},

		ForceAttemptHTTP2: true,
	}

	client := &http.Client{
		Transport: transport,
	}

	runSlowReader(client)
}

// runMultiplexing runs the earlier /slow and /fast experiment.
func runMultiplexing(client *http.Client) {
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

func runSlowReader(client *http.Client) {
	fmt.Println("\n--- Slow Reader Experiment ---")

	resp, err := client.Get("https://localhost:9500/large")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Protocol:", resp.Proto)
	fmt.Println("Status:", resp.Status)
	fmt.Println("Content-Length:", resp.ContentLength)

	buf := make([]byte, 4*1024)
	var total int64

	start := time.Now()

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)

			// Deliberately slow the application-level reader.
			time.Sleep(100 * time.Millisecond)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("read error: %v", err)
			return
		}
	}

	fmt.Printf("Bytes read: %d\n", total)
	fmt.Printf("Read duration: %s\n", time.Since(start))
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
