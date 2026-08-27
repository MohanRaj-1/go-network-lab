# TCP and Message Framing

## 1. TCP Client and Server

An IP address identifies a host and is used to route network traffic to it.

A port identifies a service or application endpoint on that host. I initially thought of a port as a doorway into a machine, which helped me understand why a client connects to a specific port.

`127.0.0.1` is the loopback address, which allows network traffic to return to the same machine.

A server provides a network service and waits for clients to connect to it. A client connects to the server to use that service.

---

## 2. Blocking I/O

Network I/O operations such as `Read` can block.

When a goroutine calls `Read` and no data is available, that goroutine waits until data arrives, an error occurs, or a deadline is reached.

This means a blocking operation can prevent the code running in that goroutine from making further progress.

---

## 3. Goroutines and Concurrent Connections

If the server handles a connection directly after `Accept`, a blocking operation such as `Read` can prevent the server from accepting another connection.

I solved this by handling each connection in a goroutine:

```go
go handleConnection(conn)
```

This allows the main goroutine to continue accepting new connections while individual connection handlers wait for network I/O.

---

## 4. Connection Errors and Cleanup

A client can disconnect unexpectedly, so a connection error should not normally crash the entire server.

Instead, the connection handler can log the error, return from the handler, and clean up the connection.

I used:

```go
defer conn.Close()
```

to ensure the connection is closed when the handler finishes.

---

## 5. Read Deadlines and Timeouts

A client can connect and then never send data.

Without a deadline, the server could remain blocked in `Read` indefinitely.

I used a read deadline to limit how long the server waits for network data.

When the deadline expires, the read returns an `i/o timeout` error and the handler can clean up the connection.

---

## 6. TCP as a Byte Stream

TCP provides an ordered byte stream.

It does not preserve the boundaries of application-level writes.

For example, if the client performs:

```go
Write("HELLO")
Write("WORLD")
```

the server cannot assume that it will receive:

```text
Read → HELLO
Read → WORLD
```

Our experiment with a small read buffer demonstrated that the server could instead observe:

```text
HEL
LO
WOR
LD
```

---

## 7. Why Message Framing Is Needed

Because TCP provides a byte stream rather than application-level messages, the receiver needs a way to determine where one message ends and another begins.

This is called message framing.

Common approaches include delimiters and length-prefixed messages.

---

## 8. Length-Prefixed Framing

I implemented a length-prefixed framing protocol.

Each message consists of:

```text
[4-byte length][payload]
```

For example:

```text
[00 00 00 05][HELLO]
```

The server first reads the four-byte length, validates that the message is within the allowed maximum size, and then reads exactly that many payload bytes.

I used big-endian encoding for the length.

---

## 9. Partial Reads

A single TCP `Read` is not guaranteed to return an entire application message.

The receiver may receive only part of the data.

For example, if the length prefix says the message is five bytes long, the server might initially receive:

```text
HE
```

and still need:

```text
LLO
```

I used `io.ReadFull` so the server continues reading until the requested number of bytes has been received or an error occurs.

---

## 10. Multiple Messages Over One Connection

A TCP connection can carry multiple application-level messages.

Our framing server repeatedly reads:

```text
[length][payload]
[length][payload]
...
```

over the same TCP connection.

I demonstrated this by sending:

```text
HELLO
WORLD
```

and receiving both messages from the same connection.

This gives us a persistent connection carrying multiple framed messages.

---

## 11. Experiments

### Experiment 1.1 — TCP Server and Client

Created a small TCP server on port `8080`.

Used `curl` to connect to it and observed the HTTP request sent by the client.

The server then wrote an HTTP response back to the client, and `curl -v` allowed us to inspect the HTTP response headers.

This helped me understand that HTTP data can be transferred over a TCP connection.

### Experiment 1.2 — Blocking Connections

Started multiple clients and observed that when the server handled a connection synchronously, it could not accept another connection while blocked reading from the first client.

Changed the connection handler to run in a goroutine and observed that the server could accept multiple clients while their handlers were waiting for data.

### Experiment 1.3 — Errors, Cleanup and Deadlines

Disconnected clients intentionally and observed network errors.

Instead of allowing a client-specific error to crash the server, the handler logs the error and returns.

Added read deadlines and demonstrated that a server does not have to wait indefinitely for a client to send data.

### Experiment 1.4 — TCP Byte Stream

Used a small three-byte read buffer and sent `HELLO` and `WORLD`.

Observed:

```text
HEL
LO
WOR
LD
```

This demonstrated that TCP is a byte stream and does not preserve application message boundaries.

### Experiment 2.1 — Building Our Own Framing Protocol

Built a framing server and client using a four-byte length prefix followed by the payload.

The server validates the message length before allocating the payload buffer and then reads the complete payload.

### Experiment 2.2 — Partial Frames and Deadlines

Deliberately split the length prefix and payload across multiple writes.

The server successfully reconstructed the complete message using the length prefix and `io.ReadFull`.

Also tested an incomplete message and observed that the read eventually failed with an `i/o timeout`.

---

## 12. What I Learned

Through these experiments I learned how a basic TCP server works, how
blocking network I/O behaves, how goroutines allow multiple connections
to be handled concurrently, how connection failures should be handled,
why deadlines are important, and why application protocols need message
framing when built on top of TCP.

The most important lesson for me was understanding that TCP provides a
reliable byte stream, not application-level messages.

I initially assumed that a single `Read` would give me the message that
the client had written. By deliberately using a small read buffer and
fragmenting the data sent by the client, I observed that the receiver
could get only part of a message.

That led me to understand why framing is necessary. I then implemented
a length-prefixed framing protocol and verified that the server could
reconstruct a complete message even when the underlying TCP data arrived
in multiple pieces.

This experiment changed my mental model of TCP from:

    "the client sends a message and the server reads the message"

to:

    "TCP provides a stream of bytes, and the application protocol defines
    how those bytes are interpreted as messages."

That is the foundation I will build on in the next networking
experiments.
