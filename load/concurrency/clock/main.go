// Clock probes the observed granularity of time.Now on this machine.
// It measures clock reads, not request latency or guaranteed timer resolution.
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	const readings = 1_000_000
	previous := time.Now()
	var equal, negative, positive int
	var smallest, largest time.Duration
	for i := 0; i < readings; i++ {
		now := time.Now()
		delta := now.Sub(previous)
		previous = now
		switch {
		case delta == 0:
			equal++
		case delta < 0:
			negative++
		default:
			positive++
			if smallest == 0 || delta < smallest {
				smallest = delta
			}
			if delta > largest {
				largest = delta
			}
		}
	}
	fmt.Printf("Environment: %s %s/%s GOMAXPROCS=%d\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0))
	fmt.Printf("Consecutive clock intervals: %d\nIdentical readings: %d\nNegative intervals: %d\nPositive intervals: %d\n", readings, equal, negative, positive)
	if positive > 0 {
		fmt.Printf("Smallest positive interval: %s\nLargest positive interval: %s\n", smallest, largest)
	} else {
		fmt.Println("Positive interval range: N/A")
	}
	fmt.Println("Observed intervals include loop overhead and scheduling; they do not establish request latency or guaranteed clock resolution.")
}
