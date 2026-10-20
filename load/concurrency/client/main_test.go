package main

import (
	"bufio"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestTimedRequestCompletedExchange(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	deadline := time.Now().Add(5 * time.Second)
	if err := client.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := server.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		request, err := bufio.NewReader(server).ReadString('\n')
		if err == nil && request != "WORK\n" {
			err = fmt.Errorf("unexpected request %q", request)
		}
		if err == nil {
			_, err = fmt.Fprint(server, "DONE 01234567\n")
		}
		done <- err
	}()
	before := time.Now()
	response, completedAt, elapsed, err := timedRequest(client, bufio.NewReader(client))
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if response != "DONE 01234567\n" {
		t.Fatalf("response = %q", response)
	}
	if elapsed < 0 || elapsed > after.Sub(before) {
		t.Fatalf("elapsed = %v, enclosing interval = %v", elapsed, after.Sub(before))
	}
	if completedAt.Before(before) || completedAt.After(after) {
		t.Fatalf("completion time is outside the exchange interval")
	}
}

func TestLatencyStatsRecordedSamples(t *testing.T) {
	for _, tc := range []struct {
		name    string
		samples []time.Duration
		want    [4]time.Duration
	}{
		{"empty", nil, [4]time.Duration{}},
		{"one", []time.Duration{7}, [4]time.Duration{7, 7, 7, 7}},
		{"unsorted positive", []time.Duration{9, 3, 7, 5}, [4]time.Duration{3, 6, 6, 9}},
		{"recorded zero", []time.Duration{9, 0, 3}, [4]time.Duration{0, 4, 3, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]time.Duration(nil), tc.samples...)
			min, avg, p50, p95 := latencyStats(tc.samples)
			if got := [4]time.Duration{min, avg, p50, p95}; got != tc.want {
				t.Fatalf("statistics = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.samples, original) {
				t.Fatal("statistics mutated the recorded samples")
			}
		})
	}
}
