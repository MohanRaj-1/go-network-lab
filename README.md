# Go Network Lab

A hands-on Go networking laboratory exploring networking fundamentals through progressively more complex experiments and networked systems.

## Goals

- Understand computer networking through practical experiments
- Learn how networking protocols work internally
- Implement networking concepts using Go
- Measure and analyze network behavior
- Build reliable networked systems
- Document findings and experiments

## Topics

- TCP
- UDP
- DNS
- HTTP
- HTTPS / TLS
- WebSockets
- HTTP/2
- gRPC
- Concurrency
- Reliability
- Network Performance

## Project Structure

```text
tcp/
├── echo/       # Basic TCP client/server experiment
├── framing/    # Length-prefixed message framing
└── protocol/   # Application protocol over framed TCP

protocol/       # Shared application protocol package

docs/
└── learnings/  # Notes, experiments, and observations
```

## Learning Progress

### Phase 1 — TCP Fundamentals

- Raw TCP server and client
- Blocking I/O
- Concurrent connections with goroutines
- Connection errors and cleanup
- Read deadlines and timeouts
- TCP as a byte stream
- Length-prefixed message framing
- Partial reads and `io.ReadFull`
- Multiple messages over one connection

See [`docs/learnings/01-tcp-and-framing.md`](docs/learnings/01-tcp-and-framing.md).

### Phase 2 — Application Protocol

- JSON request/response protocol
- Protocol versioning
- Request IDs
- `PING`, `ECHO`, and `TIME`
- Protocol validation
- Application-level error responses
- Persistent TCP connections
- Protocol and connection lifecycle tests

See [`docs/learnings/02-application-protocol.md`](docs/learnings/02-application-protocol.md).

### Upcoming

The lab will continue with HTTP, HTTPS/TLS, DNS, UDP, HTTP/2, WebSockets, gRPC, service-to-service networking, and networking under load.

## Project Status

🚧 In Development
