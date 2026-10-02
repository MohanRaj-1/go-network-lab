# UDP Fundamentals

## 1. Introduction

UDP (User Datagram Protocol) is a transport-layer protocol that provides
datagram-based communication between applications.

Unlike TCP, UDP does not provide a reliable byte stream. It preserves
datagram boundaries, but it does not provide guarantees for delivery,
ordering, retransmission, or duplicate suppression.

The goal of this phase was not to build a replacement for TCP.

The goal was to understand:

- How UDP differs from TCP
- How UDP datagrams behave on the wire
- How UDP is used from Go
- How datagram boundaries differ from TCP byte streams
- What happens when receive buffers are too small
- How multiple clients communicate with one UDP socket
- What UDP does and does not guarantee
- How an application can add request IDs, timeouts, retries, and
  duplicate detection when required

---

## 2. UDP Mental Model

### TCP

TCP provides a reliable, ordered byte stream.

For example, if an application sends:

    HELLO
    WORLD

the receiver does not receive "messages" called `HELLO` and `WORLD`.

It receives a stream of bytes:

    HELLOWORLD

A single `Read` may return part of the data, all of it, or data spanning
multiple application writes.

Therefore, applications using TCP need some form of framing when they
need message boundaries.

### UDP

UDP is datagram-oriented.

Each call that sends a datagram represents one UDP datagram, and the
receiver reads datagrams rather than an undifferentiated byte stream.

During the experiment, sending:

    HELLO
    WORLD

as two separate UDP writes resulted in the server receiving:

    HELLO
    WORLD

as two separate datagrams.

This demonstrated an important difference:

    TCP → byte stream
    UDP → datagrams

UDP therefore preserves datagram boundaries while TCP does not preserve
application message boundaries.

### UDP does not mean "TCP but faster"

UDP and TCP provide different transport semantics.

UDP does not perform TCP's connection establishment, retransmission,
ordering, or reliable delivery.

This makes UDP useful when an application prefers lightweight datagram
communication or needs to define its own delivery semantics.

---

## 3. UDP Wire Format

A UDP datagram contains an 8-byte UDP header followed by the payload.

The header contains:

| Field            |    Size |
| ---------------- | ------: |
| Source Port      | 16 bits |
| Destination Port | 16 bits |
| Length           | 16 bits |
| Checksum         | 16 bits |

### Length

The UDP Length field represents the total UDP datagram size:

    UDP header + payload

For example, if the UDP Length is 18 bytes:

    Header  = 8 bytes
    Payload = 10 bytes

### Checksum

The checksum is used to detect corruption.

It does not provide:

- Reliable delivery
- Retransmission
- Ordering
- Duplicate suppression

Those are separate concerns.

---

## 4. UDP in Go

Go provides UDP support through the `net` package.

The main APIs explored during this phase were:

- `net.ListenUDP`
- `net.DialUDP`
- `net.UDPConn`
- `ReadFromUDP`
- `WriteToUDP`

### `net.ListenUDP`

The server used:

    net.ListenUDP("udp", addr)

This binds a UDP socket to a local address.

For example:

    127.0.0.1:9000

The socket can then receive datagrams from multiple clients.

### `ReadFromUDP`

The server used:

    n, clientAddr, err := conn.ReadFromUDP(buf)

This provides:

- Number of bytes received
- Sender's UDP address
- Any error

The sender address is important because a single UDP socket can receive
datagrams from different clients.

### `WriteToUDP`

The server sends a datagram to a particular destination using:

    conn.WriteToUDP(data, clientAddr)

This allows the server to reply to the client that sent the datagram.

### `net.DialUDP`

The client used:

    net.DialUDP("udp", nil, serverAddr)

This associates the UDP socket with a default remote peer.

This does not turn UDP into TCP.

There is still:

- No TCP handshake
- No retransmission
- No ordering guarantee
- No delivery guarantee

It simply allows the client to use operations such as `Write` and `Read`
with a default peer.

---

## 5. Datagram Boundaries and Receive Buffers

One experiment used a server receive buffer smaller than the incoming
datagram.

The client sent:

    HELLO

while the server used a 3-byte receive buffer.

On Windows, the observed result was:

    n = 3
    data = "HEL"
    error = message larger than the receive buffer

This demonstrated that the receive buffer must be large enough for the
datagrams the application expects.

The exact behavior and error reporting can depend on the operating system
and networking API.

Therefore, the experiment should not be interpreted as a universal rule
that every operating system reports UDP truncation in exactly the same
way.

With a buffer large enough to receive the datagram, the complete:

    HELLO

was received successfully.

---

## 6. Multiple UDP Clients

A single UDP server socket was tested with multiple clients.

The server received datagrams similar to:

    HELLO  from 127.0.0.1:<port1>
    WORLD  from 127.0.0.1:<port2>
    UDP    from 127.0.0.1:<port3>

This demonstrated that a UDP server does not need a separate listening
socket for every client.

The sender address returned by `ReadFromUDP` allows the server to identify
where a datagram came from and send a response to that address.

---

## 7. UDP Failure Characteristics

UDP does not provide the reliability mechanisms provided by TCP.

An application using UDP must account for the possibility of:

- Lost datagrams
- Reordered datagrams
- Duplicate datagrams

UDP itself does not provide:

- Retransmission
- Delivery confirmation
- Ordering
- Duplicate suppression

Whether an application needs to handle these conditions depends on the
application.

Not every UDP application needs reliable delivery.

For applications that do require reliability, those semantics have to be
implemented at a higher layer.

---

## 8. Application-Level Reliability

To understand how an application can add reliability, a small request/
response protocol was built on top of UDP.

The protocol contains:

### Request

    ID
    Operation
    Payload

### Response

    ID
    Status
    Payload

JSON was used for this experiment because custom binary framing had
already been explored earlier in the networking lab.

The purpose here was to focus on UDP behavior and application-level
reliability rather than repeat the binary protocol work.

---

## 9. Request IDs

A request ID allows the client to associate a response with a request.

For example:

    Request:
        ID = 42
        Operation = TIME

    Response:
        ID = 42
        Status = OK

The response validation checks that the response ID matches the request.

A response with a different ID is rejected.

This becomes especially important when requests can be retried.

---

## 10. Timeout and Retry

Because UDP does not guarantee that a response will arrive, the client
cannot wait indefinitely.

The client used a read deadline:

    conn.SetReadDeadline(time.Now().Add(2 * time.Second))

If no response arrived before the deadline, the read returned a timeout.

An important distinction is:

    timeout != proof that the request was lost

A timeout only tells the client that a response was not received before
the deadline.

Several things could have happened:

- The request was lost.
- The server received the request but the response was lost.
- The server was delayed.
- The response arrived after the deadline.

The client therefore retried the request using the same request ID.

---

## 11. Duplicate Requests

Retrying introduces another problem.

Consider:

    Client → Request 42 → Server

The server processes the request.

    Server → Response 42 → Client

But the response is lost.

The client does not know whether the server processed the request, so it
retries:

    Client → Request 42 → Server

Without duplicate detection, the server might execute the operation twice.

Therefore, the server keeps the result of previously processed requests.

The cache key used in this experiment was:

    (client address, request ID)

This means:

    same client + same ID
        → duplicate

while:

    different client + same ID
        → new request

and:

    same client + different ID
        → new request

This was an experimental identity model for the learning exercise. A UDP
source address is not a universal application-level identity in a
production system.

---

## 12. Cached Response Experiment

The server was tested with a simulated lost first response.

The sequence was:

    Client
       |
       | Request ID 42
       v
    Server
       |
       | Process request
       | Store response
       |
       X Response lost
       |
    Client
       |
       | timeout
       |
       | Retry ID 42
       v
    Server
       |
       | Detect duplicate
       | Return cached response
       v
    Client

The important property is that the second request did not execute the
operation again.

The original response was returned from the cache.

A focused test verified this behavior by controlling the time source and
counting how many times the operation was executed.

The test also verified that:

- A retry from the same client with the same ID is a duplicate.
- The cached result is reused.
- A different client using the same ID is not a duplicate.
- The same client using a different ID is not a duplicate.

---

## 13. Protocol Validation

JSON syntax validation alone is not sufficient.

A syntactically valid request can still be semantically invalid.

For example:

    {"id":0,"operation":"TIME"}

is valid JSON but is not a valid request for this protocol because request
IDs must be positive.

The protocol therefore validates:

- Request ID
- Operation

Responses are also validated for:

- Response ID
- Correlation with the original request
- Response status

The supported response statuses are:

    OK
    ERROR

---

## 14. Testing

The protocol tests cover:

- Request round trips
- Response round trips
- Invalid JSON
- Request validation
- Response correlation
- Invalid response IDs
- Invalid response statuses
- Duplicate request handling
- Cached response reuse
- Client separation
- Request ID separation

The duplicate-request test is particularly important because it verifies
behavior rather than simply checking that a map contains an entry.

It verifies that the operation is executed once and the same result is
returned for a retry.

The full repository test suite passes with:

    go test ./...

---

## 15. What I Learned

### UDP is fundamentally different from TCP

The biggest lesson from this phase is that UDP is not simply a faster
version of TCP.

TCP provides a reliable ordered byte stream.

UDP provides datagrams with much fewer transport-level guarantees.

### Datagram boundaries matter

With TCP, application-level framing is required when message boundaries
matter.

With UDP, datagram boundaries are preserved by the transport.

### Reliability moves upward

If an application needs:

- Request correlation
- Timeouts
- Retries
- Duplicate detection
- Ordering
- Reliable delivery

then those semantics must be provided by the application or another
protocol layer.

### Retries create new problems

A timeout followed by a retry can create duplicate requests.

Therefore, adding retries without considering duplicate processing can
cause an operation to execute more than once.

Request IDs and duplicate detection can be used to address this problem.

### Experiments are useful for understanding networking

The UDP experiments were more useful than simply reading API
documentation.

In particular, the experiments demonstrated:

- Datagram boundaries
- Sender addresses
- Receive-buffer behavior
- Timeouts
- Retries
- Duplicate requests
- Cached responses

---

## 16. Scope and Limitations

This phase intentionally does not implement a reliable transport protocol.

The implementation does not provide:

- Persistent request caches
- Cache expiration
- Distributed duplicate detection
- Advanced retry policies
- Congestion control
- Ordered delivery
- Reliable UDP
- Packet-loss simulation infrastructure
- Production-grade request identity

The purpose was to understand UDP and demonstrate how an application can
add a small amount of reliability when required.

Building a complete reliable transport over UDP would duplicate concepts
already provided by TCP and would expand the scope of this networking lab
beyond the intended learning objective.

---

## 17. Phase 6 Completion

Phase 6 demonstrated the fundamentals of UDP and how application-level
protocols can build additional semantics on top of it.

The phase covered:

- UDP datagrams
- UDP wire format
- TCP vs UDP
- UDP APIs in Go
- Datagram boundaries
- Multiple clients
- Failure characteristics
- Timeouts
- Retries
- Request IDs
- Duplicate detection
- Cached responses
- Protocol validation
- Focused testing

The next phase is:

    Phase 7 — Networking Under Load
