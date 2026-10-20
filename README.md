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

udp/
├── echo/       # UDP echo client/server experiment
└── protocol/   # Small application protocol over UDP

protocol/       # Shared application protocol package

load/
├── concurrency/            # CPU-bound throughput and latency experiments
├── backpressure/queue/     # Bounded queue with blocking and rejection policies
├── resources/connections/  # Idle connection and Go runtime resource measurements
└── protection/connections/ # Admission limits, idle deadlines, and slot reuse

internal/
├── rawhttp/    # HTTP parsing and connection handling
└── dns/        # DNS encoding, decoding, validation, UDP and TCP lookup
    └── client/ # Runnable IPv4 lookup example

http2/
└── fundamentals/ # HTTP/2 multiplexing, framing, flow control, and HPACK experiments
    ├── client/   # Shared transport, slow reads, repeated requests, and TLS key logging
    └── server/   # /slow, /fast, and chunked writes for a 4 MiB /large response

websocket/
└── handshake/        # WebSocket handshake, messaging, frames, and lifecycle experiments
    ├── client/       # Controlled payload lengths, ping/pong, and closure experiments
    ├── concurrent_client/ # Three independent clients coordinated with a channel barrier
    └── server/       # /ws echo endpoint with handshake and close-status logging

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

### Phase 6 — UDP Fundamentals

Status: Complete

Implemented and explored:

- UDP datagrams and the UDP mental model
- UDP wire format and the 8-byte header
- TCP byte streams vs UDP datagrams
- `net.ListenUDP`, `net.DialUDP`, `ReadFromUDP`, and `WriteToUDP`
- Datagram boundaries and Windows receive-buffer behavior
- Multiple clients communicating with one UDP socket
- UDP loss, reordering, and duplication characteristics
- JSON request/response protocol with `TIME` and unknown-operation errors
- Application-level request IDs and response correlation
- Read deadlines and one retry using the same request ID
- Duplicate detection and cached responses
- Semantic protocol validation
- Focused tests for serialization, validation, and duplicate handling
- Cache separation by client address and request ID

See [`docs/learnings/08-udp-fundamentals.md`](docs/learnings/08-udp-fundamentals.md).

### Phase 7 — Networking Under Load

Status: Complete

Implemented and explored:

- CPU-bound throughput and latency under increasing client concurrency
- Closed-loop measurements with one request in flight per client
- CPU saturation, diminishing throughput returns, and rising tail latency
- Little's Law as a consistency check for throughput and average latency
- Bounded queues with blocking and rejection policies
- Immediate retries and retry pressure
- Resource costs of up to 10,000 idle TCP connections in this environment
- Application admission limits and explicit `OK` / `BUSY` responses
- Rolling idle read deadlines, resource reclamation, and two-wave slot reuse
- Final synthesis of capacity, waiting, resource use, and protection
- Measurement limitations and the distinction between experimental results and production capacity

See [`docs/learnings/09-networking-under-load.md`](docs/learnings/09-networking-under-load.md).

### Phase 8 — HTTP/2 Fundamentals

Status: Complete

Implemented and explored:

- Go's standard `net/http` HTTP/2 support over TLS, with explicit protocol verification
- Single-connection HTTP/1.1 baseline compared with HTTP/2 multiplexing of `/slow` and `/fast`
- Decrypted Wireshark captures using TLS key logging
- Binary frame headers, SETTINGS, stream IDs, END_HEADERS, and END_STREAM
- A 4 MiB `/large` response and deliberately slow application reads
- Stream-level and connection-level flow-control credit and WINDOW_UPDATE observations
- Repeated requests over one connection to inspect HPACK static and dynamic tables

Key lessons:

- HTTPS alone does not establish that HTTP/2 was negotiated.
- Multiplexing lets independent exchanges progress on one connection, while TCP's ordered delivery remains a shared constraint.
- Captured window updates demonstrate credit replenishment; the slow-reader experiment did not establish a zero-window stall.
- HPACK dynamic-table reuse reduced the observed request header-block fragment from 41 to 9 bytes; this is a capture-specific result.
- Application timings, packet observations, and protocol reasoning support different conclusions and need explicit limitations.

The experiments use Go's HTTP/2 implementation rather than a custom protocol stack.
Detailed setup, measurements, capture interpretations, and limitations are in
[`docs/learnings/10-http2-fundamentals.md`](docs/learnings/10-http2-fundamentals.md).

### Phase 9 — WebSocket Fundamentals

Status: Complete

Implemented and explored:

- Go `net/http` WebSocket server and clients using `github.com/coder/websocket`
- HTTP Upgrade handshake, `101 Switching Protocols`, and `Sec-WebSocket-Accept` verification
- Rejection of an ordinary HTTP GET without the required upgrade headers
- Multiple sequential text-message exchanges over one established connection
- Wireshark inspection of FIN, opcodes, masking, and the 125/126-byte payload-length boundary
- Ping/pong control frames and the active-reader requirement for `Conn.Ping`
- Graceful Close-frame exchanges compared with abrupt termination and EOF
- Three independent client connections coordinated with `ready` and `start` channels
- Connection results and a shared deadline to abort the barrier if a client fails to connect
- Client and server logs checked alongside individual Wireshark TCP streams

Key lessons:

- After the upgrade, messages use WebSocket frames without another HTTP handshake.
- Client masking does not provide encryption or authentication.
- Observing a pong on the wire does not establish that the client processed it; adding a background reader resolved the ping timeout in this test.
- Graceful protocol closure and immediate connection termination produce different frame and error observations.
- A synchronization barrier provides a common release point; three successful clients do not establish simultaneous execution or production scalability.

The experiments use a local, unencrypted `ws://` connection rather than a custom WebSocket implementation.
Detailed observations, the ping-timeout investigation, and limitations are in
[`docs/learnings/11-websockets-fundamentals.md`](docs/learnings/11-websockets-fundamentals.md).

### Upcoming

Future experiments include gRPC and service-to-service networking.

## Running the HTTP/2 Experiments

Start the server from the repository root:

```bash
go run ./http2/fundamentals/server
```

In a second terminal, run one of the experiments:

```bash
# HTTP/2 slow-reader and flow-control observation
go run ./http2/fundamentals/client -experiment=slow-reader

# HTTP/2 multiplexing
go run ./http2/fundamentals/client -experiment=multiplexing

# HTTP/1.1 baseline with a single connection
go run ./http2/fundamentals/client -experiment=multiplexing -http2=false
```

`-experiment` defaults to `slow-reader`, `-http2` defaults to `true`, and
`-requests` defaults to `1`. Use `-requests=2` to repeat the selected experiment
sequentially through the same client and transport; for multiplexing, each run
sends one `/slow` + `/fast` pair. The slow-reader experiment requires HTTP/2.

The client uses a local self-signed certificate for these experiments.
Certificate verification is disabled in the experimental client; do not use
this configuration for production traffic.

## Running the WebSocket Experiments

Start the server from the repository root:

```bash
go run ./websocket/handshake/server
```

In a second terminal, run one of the clients:

```bash
# Sequential text exchanges, payload-length inputs, and ping/pong
go run ./websocket/handshake/client

# Three independent connections coordinated with a synchronization barrier
go run ./websocket/handshake/concurrent_client
```

Both clients connect to `ws://localhost:8080/ws`. The sequential client currently
uses abrupt closure; its graceful-close variant is retained as commented code.
The concurrent client initiates graceful closure on each connection.

For Wireshark inspection, capture on the Windows loopback interface before
running a client. Use `tcp.port == 8080` to include the handshake and TCP traffic,
or `tcp.port == 8080 && websocket` to focus on decoded WebSocket frames.

## Releases

- `v0.1.0` — Completed TCP, application protocol, raw HTTP, TLS/HTTPS, and DNS experiments.
- `v0.2.0` — Networking Fundamentals & Load Behavior; completed UDP fundamentals and networking-under-load experiments.

## Project Status

🚧 In Development

Current milestone: WebSocket Fundamentals — Phase 9 Complete
