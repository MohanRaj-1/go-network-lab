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

## Day 2 — Persistent Messaging

### Objective

Verify that a single WebSocket connection can carry multiple sequential messages in both directions after one HTTP Upgrade handshake.

### Implementation

- Changed the server to repeatedly read a message and write an echo response until a read or write error ends the handler.
- Preserved the handshake logging and deferred `conn.CloseNow()` cleanup.
- Changed the client to send three text messages through one connection, reading each echo response before sending the next message.
- Kept the client's connection setup and handshake verification.

### Message Exchanges

The client received `101 Switching Protocols`, then completed these exchanges:

| Client message | Server response |
| --- | --- |
| `Hello from client` | `Echo: Hello from client` |
| `Second message on the same connection` | `Echo: Second message on the same connection` |
| `Third message on the same connection` | `Echo: Third message on the same connection` |

The server logged one successful handshake followed by all three messages and their responses. Each response had message type `MessageText`.

### Connection Reuse and Wireshark Observations

The supplied capture review identified one `GET /ws` request and one `101 Switching Protocols` response, followed by three client text frames alternating with three server text frames on the same TCP connection.

The connection used client port `62078` and server port `8080`. This port pair stayed the same between messages. Source and destination ports reversed when the direction of traffic changed; that reversal does not indicate a new connection. Packets must be compared within the same TCP stream to avoid confusing unrelated connections with this experiment.

The server's `conn.Read` waits for another WebSocket message on the established connection. After the upgrade, these exchanges use WebSocket frames and require no additional HTTP GET requests.

### Masking and Connection Closure

The capture review identified the client text frames as `[MASKED]` and the server text frames as unmasked. Masking does not encrypt the payload; the local `ws://` traffic remains inspectable.

After the third exchange, the client finished and its deferred `conn.CloseNow()` closed the connection without a WebSocket close handshake. The server's next read ended with:

```text
WebSocket read ended: failed to get reader: failed to read frame header: EOF
```

This is consistent with the client closing the underlying connection after completing its exchanges. Graceful WebSocket closure and lifecycle handling remain for a later experiment.

### Key Learnings

1. One successful handshake can establish a connection that carries many WebSocket messages.
2. Repeated messages reuse the same TCP connection and port pair.
3. After the upgrade, application messages use WebSocket frames rather than new HTTP requests.
4. Client frames were masked, while server frames were unmasked.
5. A WebSocket message may occupy one frame or multiple frames when fragmented; messages and frames are distinct concepts.
6. This sequential client demonstrates repeated exchanges in both directions. It does not demonstrate both sides independently sending messages simultaneously.

### Evidence and Limitations

These findings record the supplied client output, server logs, and Wireshark capture review. The client waited for each response before sending its next message. Simultaneous bidirectional messaging, fragmentation, control-frame behavior, and graceful connection shutdown were not evaluated in this experiment.
