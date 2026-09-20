package rawhttp

import (
	"fmt"
	"strings"
)

type Request struct {
	Method  string
	Target  string
	Version string
	Headers map[string][]string
	Body    []byte
}

func validateRequestSemantics(request Request) error {
	hosts := request.Headers["host"]
	if len(hosts) != 1 {
		return fmt.Errorf("expected exactly one Host header, got %d", len(hosts))
	}
	if strings.TrimSpace(hosts[0]) == "" {
		return fmt.Errorf("Host header must be nonempty")
	}
	return nil
}

func printRequest(request Request) {
	fmt.Printf("method: %s\ntarget: %s\nversion: %s\n\nheaders:\n", request.Method, request.Target, request.Version)
	for name, values := range request.Headers {
		for _, value := range values {
			fmt.Printf("%s: %s\n", name, value)
		}
	}
	fmt.Printf("\nbody:\n%s\n", request.Body)
}

func shouldClose(request Request) bool {
	keepAlive := false
	for _, value := range request.Headers["connection"] {
		for _, token := range strings.Split(value, ",") {
			switch strings.ToLower(strings.TrimSpace(token)) {
			case "close":
				return true
			case "keep-alive":
				keepAlive = true
			}
		}
	}
	return request.Version != "HTTP/1.1" && !keepAlive
}
