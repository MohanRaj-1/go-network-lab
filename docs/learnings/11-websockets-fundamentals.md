# WebSocket Fundamentals

## Day 1 — HTTP Upgrade Handshake

### Objective

Understand how a WebSocket connection is established using an HTTP Upgrade handshake, and verify the exchange using Go application logs and Wireshark.

### Implementation

- Created a WebSocket server using Go's `net/http` and `github.com/coder/websocket`.
- Created a client that establishes a WebSocket connection, sends a text message, and reads the server's response.
- Added logging for the incoming HTTP request and selected handshake headers.

### Successful Handshake

The captured request was `GET /ws HTTP/1.1`. It included:

- `Connection: Upgrade`
- `Upgrade: websocket`
- `Sec-WebSocket-Key`
- `Sec-WebSocket-Version: 13`

The server responded with `HTTP/1.1 101 Switching Protocols`, along with the `Upgrade`, `Connection`, and `Sec-WebSocket-Accept` headers.

Wireshark identified the request in frame 6 and its response in frame 8. The response followed the request by approximately 580 microseconds in this capture.

### Accept Calculation

The server derives `Sec-WebSocket-Accept` using the client's key text and the WebSocket GUID:

`258EAFA5-E914-47DA-95CA-C5AB0DC85B11`

It concatenates these values, calculates the SHA-1 digest, and Base64-encodes the digest.

The accept value in the packet capture matched the value printed by the Go client.

### Negative Test

Sending an ordinary HTTP GET request to `/ws` using `curl` returned `426 Upgrade Required`. The server logged a handshake failure because the required upgrade headers were missing.

This demonstrates that an ordinary HTTP request is not sufficient to establish a WebSocket connection.

### Key Learnings

1. The classic WebSocket handshake starts with an HTTP GET request over TCP.
2. The client requests a protocol switch using the Upgrade headers.
3. A successful handshake returns `101 Switching Protocols`.
4. The handshake reuses the established TCP connection.
5. After the upgrade, application messages are exchanged using WebSocket frames.
6. The `Sec-WebSocket-Accept` calculation validates the handshake procedure; it is not authentication.

### Evidence and Limitations

The handshake was verified using application logs, client output, and Wireshark captures on the Windows loopback interface.

The captured timing and packet sizes are observations from this environment, not general performance guarantees. The experiment uses an unencrypted local `ws://` connection for inspection and does not evaluate TLS or production authentication.
