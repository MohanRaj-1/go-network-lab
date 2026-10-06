package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

type Job struct {
	Done chan struct{}
}

type Counters struct {
	Accepted  atomic.Int64
	Queued    atomic.Int64
	Completed atomic.Int64
	Waiting   atomic.Int64
	Rejected  atomic.Int64
}

func main() {
	policy := flag.String("policy", "block", "queue-full policy: block or reject")
	flag.Parse()
	if *policy != "block" && *policy != "reject" {
		panic("policy must be block or reject")
	}
	listener, err := net.Listen("tcp", ":9200")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	jobs := make(chan Job, 4)
	var counters Counters
	go worker(jobs, &counters)
	go report(jobs, &counters)
	fmt.Println("listening on :9200; workers=1; queue=4; processing=100ms")
	fmt.Println("queue-full policy:", *policy)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			continue
		}
		go handleConnection(conn, jobs, &counters, *policy)
	}
}

func handleConnection(conn net.Conn, jobs chan<- Job, counters *Counters, policy string) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		request, err := reader.ReadString('\n')
		if err != nil || request != "WORK\n" {
			return
		}
		// Accepted counts valid requests, not TCP connections.
		counters.Accepted.Add(1)
		job := Job{Done: make(chan struct{})}
		if policy == "reject" {
			select {
			case jobs <- job:
				// Submission succeeds immediately when space is available.
			default:
				counters.Rejected.Add(1)
				if _, err := conn.Write([]byte("BUSY\n")); err != nil {
					return
				}
				continue
			}
		} else {
			counters.Waiting.Add(1)
			jobs <- job // Blocks while the bounded queue is full.
			counters.Waiting.Add(-1)
		}
		counters.Queued.Add(1)

		<-job.Done
		if _, err := conn.Write([]byte("DONE\n")); err != nil {
			return
		}
	}
}

func worker(jobs <-chan Job, counters *Counters) {
	for job := range jobs {
		// Artificial processing makes queue behavior visible; this is not CPU work.
		time.Sleep(100 * time.Millisecond)
		counters.Completed.Add(1)
		close(job.Done)
	}
}

func report(jobs <-chan Job, counters *Counters) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		// Queue length and waiting senders are instantaneous observations.
		fmt.Printf("accepted=%d queued=%d completed=%d rejected=%d queue=%d/%d submitting=%d\n",
			counters.Accepted.Load(), counters.Queued.Load(), counters.Completed.Load(),
			counters.Rejected.Load(),
			len(jobs), cap(jobs), counters.Waiting.Load())
	}
}
