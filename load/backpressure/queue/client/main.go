package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

type ClientResult struct {
	Completed       int
	Errors          int
	Latencies       []time.Duration
	Unfinished      int
	Rejected        int
	RejectedLatency time.Duration
}

func main() {
	n := flag.Int("clients", 1, "number of concurrent clients")
	retryDelay := flag.Duration("retry-delay", 0, "fixed delay after BUSY, e.g. 50ms")
	flag.Parse()
	if *n < 1 {
		panic("clients must be positive")
	}
	if *retryDelay < 0 {
		panic("retry-delay must not be negative")
	}
	const duration = 10 * time.Second
	start := make(chan struct{})
	results := make(chan ClientResult, *n)
	var ready, done sync.WaitGroup
	var deadline time.Time
	ready.Add(*n)
	done.Add(*n)

	for i := 0; i < *n; i++ {
		go func() {
			defer done.Done()
			var result ClientResult
			defer func() { results <- result }()

			conn, err := net.DialTimeout("tcp", "127.0.0.1:9200", 5*time.Second)
			if err != nil {
				result.Errors++
				ready.Done()
				return
			}
			defer conn.Close()
			reader := bufio.NewReader(conn)
			ready.Done()
			<-start

			// Closing start publishes the shared deadline to all clients.
			if err := conn.SetDeadline(deadline); err != nil {
				result.Errors++
				return
			}
			for time.Now().Before(deadline) {
				requestStart := time.Now()
				_, err := conn.Write([]byte("WORK\n"))
				var response string
				if err == nil {
					response, err = reader.ReadString('\n')
				}
				responseTime := time.Now()
				if err != nil {
					// The shared experiment cutoff is not a request failure.
					if timeout, ok := err.(net.Error); ok && timeout.Timeout() && !responseTime.Before(deadline) {
						result.Unfinished++
					} else {
						result.Errors++
					}
					return
				}
				if !responseTime.Before(deadline) {
					result.Unfinished++
					return
				}
				if response == "BUSY\n" {
					result.Rejected++
					result.RejectedLatency += responseTime.Sub(requestStart)
					// Keep the delay outside attempt latency and within the run window.
					if *retryDelay > 0 {
						remaining := time.Until(deadline)
						if remaining <= 0 {
							return
						}
						pause := *retryDelay
						if pause > remaining {
							pause = remaining
						}
						time.Sleep(pause)
					}
					continue
				}
				if response != "DONE\n" {
					result.Errors++
					return
				}
				result.Completed++
				result.Latencies = append(result.Latencies, responseTime.Sub(requestStart))
			}
		}()
	}

	ready.Wait()
	deadline = time.Now().Add(duration)
	close(start)
	done.Wait()

	var total ClientResult
	for i := 0; i < *n; i++ {
		result := <-results
		total.Completed += result.Completed
		total.Errors += result.Errors
		total.Unfinished += result.Unfinished
		total.Rejected += result.Rejected
		total.RejectedLatency += result.RejectedLatency
		total.Latencies = append(total.Latencies, result.Latencies...)
	}
	fmt.Println("Completed:", total.Completed)
	fmt.Println("Errors:", total.Errors)
	fmt.Println("Rejected:", total.Rejected)
	if total.Rejected > 0 {
		fmt.Println("Average rejection latency:", total.RejectedLatency/time.Duration(total.Rejected))
	}
	fmt.Println("Unfinished:", total.Unfinished)
	// Count only responses within the shared window; exclude setup and cleanup.
	fmt.Printf("Throughput: %.1f req/s\n", float64(total.Completed)/duration.Seconds())
	fmt.Println("\nSuccessful request latency:")
	if len(total.Latencies) == 0 {
		fmt.Println("  min: N/A\n  avg: N/A\n  p50: N/A\n  p95: N/A")
		return
	}
	min, avg, p50, p95 := latencyStats(total.Latencies)
	fmt.Printf("  min: %s\n  avg: %s\n  p50: %s\n  p95: %s\n", min, avg, p50, p95)
}

func latencyStats(latencies []time.Duration) (min, avg, p50, p95 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0, 0
	}
	samples := append([]time.Duration(nil), latencies...)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	var sum time.Duration
	for _, latency := range samples {
		sum += latency
	}
	// Median: the middle sample, or the midpoint of the two middle samples.
	middle := len(samples) / 2
	p50 = samples[middle]
	if len(samples)%2 == 0 {
		lower := samples[middle-1]
		p50 = lower + (p50-lower)/2
	}
	// Nearest-rank percentile: ceil(0.95 * sample count), then zero-based index.
	index := (95*len(samples)+99)/100 - 1
	return samples[0], sum / time.Duration(len(samples)), p50, samples[index]
}
