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

## Day 3 — WebSocket Frames and Control Frames

### Objective

Inspect WebSocket text-frame headers, verify the payload-length encoding boundary at 126 bytes, and observe a ping/pong exchange in Wireshark.

### Implementation

- Inspected an ordinary client text frame from the existing messaging experiment.
- Changed the client inputs to `Hello`, 125 repetitions of `A`, 126 repetitions of `B`, and 130 repetitions of `C`, using `strings.Repeat` for the controlled lengths.
- Kept the sequential send-and-read loop. The server's `Echo: ` prefix adds six bytes to each response payload.
- Added a `conn.Ping(ctx)` call after the message loop.
- Inspected traffic captured on the Windows loopback interface using `tcp.port == 8080 && websocket`.

### Ordinary Text Frame

Experiment A's frame 10 carried `Hello from client` from client port `62078` to server port `8080`:

| Field | Observed value |
| --- | --- |
| FIN | True |
| Reserved bits | 0 |
| Opcode | Text (1) |
| MASK | True |
| Payload length | 17 bytes |
| Masking key | `da9e581d` (four bytes) |

This was a final text frame carrying the complete observed message. Its TCP payload was 23 bytes: a two-byte base header, a four-byte masking key, and 17 bytes of message data.

### Payload-Length Boundary

Experiment B used client port `52636` and server port `8080`. The supplied frame details showed:

| Capture frame | Client payload | Seven-bit length indicator | Extended length | TCP payload bytes |
| --- | --- | --- | --- | --- |
| 10 | `Hello` (5 bytes) | 5 | None | 11 |
| 14 | 125 × `A` | 125 | None | 131 |
| 18 | 126 × `B` | 126 | 126 (16 bits) | 134 |
| 22 | 130 × `C` | 126 | 130 (16 bits) | 138 |

All four client frames had FIN true, opcode 1, and MASK true. At 125 bytes, the length fits directly in the seven-bit length field. At 126 bytes, the value 126 becomes a marker indicating that the actual length follows in a two-byte extended field. The 130-byte payload uses the same marker with an extended value of 130.

The length indicator is only the lower seven bits of the second header byte. For these masked client frames, the complete second byte is `0xFD` for the direct length 125 and `0xFE` for the extended-length marker 126.

Payload length excludes the WebSocket header and masking key. Frame 14 carried `2 + 4 + 125 = 131` TCP payload bytes; frame 18 carried `2 + 2 + 4 + 126 = 134`. These are observations from the supplied segments, not a general rule that every WebSocket frame occupies exactly one TCP segment.

### Ping and Pong

Experiment C showed these control frames on the connection between client port `64382` and server port `8080`:

| Field | Frame 26: client ping | Frame 28: server pong |
| --- | --- | --- |
| Direction | Client → server | Server → client |
| Opcode | Ping (9) | Pong (10) |
| FIN | True | True |
| MASK | True | False |
| Payload length | 1 byte | 1 byte |
| Payload in hexadecimal | `31` | `31` |
| TCP payload bytes | 7 | 3 |

The ping used masking key `6ba4254b`. The pong returned the same one-byte payload, `0x31`, representing ASCII `1`. This verifies a corresponding pong on the wire.

The packet evidence alone did not establish that the client's `conn.Ping(ctx)` call returned successfully. At this stage, the client had no active read after its message loop. The follow-up investigation below verifies API completion separately.

### Why Masking Is Not Encryption

Masking transforms payload bytes using a repeating four-byte key that is included in the frame itself. Anyone who can read the frame can recover the payload using that key, as Wireshark did in these captures. Masking provides neither confidentiality nor authentication. This experiment used unencrypted `ws://` traffic; TLS protection with `wss://` was not evaluated.

### Key Learnings

1. FIN, opcode, MASK, and payload length describe how a frame carries data.
2. The observed client text frames were final, masked frames with opcode 1.
3. The 125-byte payload used a direct length; the 126-byte and 130-byte payloads used marker 126 followed by a 16-bit actual length.
4. Header bytes and masking keys are excluded from the payload length.
5. The observed ping and pong used opcodes 9 and 10 and carried matching payloads. The client ping was masked; the server pong was unmasked.
6. Masking is reversible using information carried in the frame and does not encrypt the connection.

### Evidence and Limitations

These initial findings are based on the supplied Wireshark frame details from separate experiment captures; frame numbers and client ports are specific to each capture. Fragmented messages, continuation frames, the 64-bit payload-length encoding, and close frames were not investigated. The initial observations did not establish simultaneous application messaging or successful completion of the client's ping API call.

### Follow-up — Ping Timeout and Active Reader

The original client completed all four text/echo exchanges, then failed during the ping:

```text
Sending WebSocket ping
ping failed: failed to ping: failed to wait for pong: context deadline exceeded
```

This timeout was reproduced before changing the client. The successful text-message results remain valid; observing a pong in an earlier capture did not prove that the client processed it.

The pinned `github.com/coder/websocket` v1.8.15 documentation explains that [`Conn.Ping`](https://pkg.go.dev/github.com/coder/websocket@v1.8.15#Conn.Ping) sends a ping and waits for a reader to process the pong; it does not read the connection itself. [`Conn.Read`](https://pkg.go.dev/github.com/coder/websocket@v1.8.15#Conn.Read) wraps `Reader`, which handles incoming control frames. Only one reader may be active at a time.

The smallest change for this client was to call `conn.CloseRead(ctx)` after reading the last echo reply and before calling `conn.Ping(ctx)`. Despite its name, [`Conn.CloseRead`](https://pkg.go.dev/github.com/coder/websocket@v1.8.15#Conn.CloseRead) starts a background reader that processes control frames. It is appropriate here because no further application messages are expected. Unexpected data messages would close the connection with a policy violation, so this helper is not suitable if later experiments need more application reads.

The helper starts after the sequential reads finish, avoiding concurrent application readers. Its lifetime is bounded by the existing context and deferred `conn.CloseNow()` cleanup. The server and the shared ten-second timeout were unchanged.

On the verification run, all four echo exchanges succeeded and `conn.Ping(ctx)` returned `nil`, allowing the client to print:

```text
Sending WebSocket ping
WebSocket ping/pong completed successfully
```

A new Windows loopback capture, inspected with Wireshark's TShark, confirmed:

| Frame | Direction | Opcode | MASK | Payload |
| --- | --- | --- | --- | --- |
| 26 | Client `56262` → server `8080` | Ping (9) | True | `0x31` |
| 28 | Server `8080` → client `56262` | Pong (10) | False | `0x31` |

Both frames had FIN true and a one-byte payload.

The missing active reader was consistent with the observed failure and the documented reader requirement. Adding a background reader resolved the timeout in this test; the successful rerun alone does not prove that it was the only possible cause. The updated run verified both the wire exchange and API completion, without establishing simultaneous application messaging.
