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
├── protocol/   # Application protocol over framed TCP
├── http/       # HTTP/1.1 server over raw TCP
├── tls/        # TLS client/server experiments
└── https/      # HTTPS using the raw HTTP implementation

protocol/       # Shared application protocol package

internal/
├── rawhttp/    # HTTP parsing and connection handling
└── dns/        # DNS encoding, decoding, validation, UDP and TCP lookup
    └── client/ # Runnable IPv4 lookup example

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

### Phase 3 — HTTP from Raw TCP

Status: Complete

Implemented:

- HTTP/1.1 request parsing and request line validation
- Header parsing, repeated headers, validation, and limits
- Host validation
- Content-Length framing and chunked transfer encoding
- Persistent connections and HTTP pipelining
- Fragmented TCP reads and concurrent connections
- HTTP responses

See [`docs/learnings/03-http-from-raw-tcp.md`](docs/learnings/03-http-from-raw-tcp.md).

### Phase 4 — TLS and HTTPS

Status: Complete

Implemented:

- TLS client/server experiments and certificate verification
- HTTPS integration with the existing raw HTTP implementation
- Separation of TLS transport protection from HTTP parsing

See [TLS Concepts and Experiments](docs/learnings/04-tls-concepts-and-experiments.md) and [HTTPS and TLS Integration](docs/learnings/05-https-and-tls.md).

### Phase 5 — DNS Client over UDP and TCP

Status: Complete

Implemented:

- DNS A/IN query encoding
- Header, question, and resource-record decoding
- Domain-name encoding and compression pointers
- Transaction ID and response validation
- DNS response-code errors and IPv4 address extraction
- UDP lookups and TCP fallback
- DNS-over-TCP length-prefixed framing
- Shared lookup deadline and context cancellation
- Unit tests and local UDP/TCP integration tests

See [`docs/learnings/06-dns-concepts-and-wire-format.md`](docs/learnings/06-dns-concepts-and-wire-format.md) and [`docs/learnings/07-dns-client-implementation.md`](docs/learnings/07-dns-client-implementation.md).

### Upcoming

Future experiments include HTTP/2, WebSockets, gRPC, service-to-service networking, and networking under load. TLS/HTTPS and DNS over UDP/TCP are documented in the completed experiments above.

## Project Status

🚧 In Development
