package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/MohanRaj-1/go-network-lab/internal/dns"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "DNS lookup failed:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	const server = "1.1.1.1:53"
	const name = "example.com"
	fmt.Printf("Looking up %s using %s\n", name, server)

	addresses, err := dns.LookupA(ctx, server, name)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		fmt.Println("No matching IPv4 addresses found.")
		return nil
	}
	for _, address := range addresses {
		fmt.Println(address)
	}
	return nil
}
