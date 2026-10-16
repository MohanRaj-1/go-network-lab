# HTTP/2 Fundamentals

## Day 1 — Why HTTP/2?

### Goal and Hypothesis

Understand how two requests share a connection under HTTP/1.1 and HTTP/2.
With one HTTP/1.1 connection, a slow request should delay another request
waiting for that connection. HTTP/2 should let separate streams make progress
concurrently over the same connection.

### Why HTTP/2 Exists

An application often needs several resources concurrently. HTTP/1.1 clients
can open multiple connections to serve that demand, but doing so adds connection
management and setup costs. HTTP/2 introduces binary framing and multiplexed
streams, so several request/response exchanges can share a connection.

### Experimental Setup

The [server](../../http2/fundamentals/server/main.go) listens on `:9500`:

- `/slow` sleeps for two seconds before returning `200 OK`.
- `/fast` returns `200 OK` without an intentional delay.
- Both handlers log method, path, and handler duration.

The server uses `http.ListenAndServeTLS` and reuses the certificate and key
from the earlier TLS experiment in `tcp/tls/basic/certs/`.
TLS provides the HTTPS connection over which the client and server negotiate
HTTP/2 through ALPN (Application-Layer Protocol Negotiation).

The [client](../../http2/fundamentals/client/main.go) shares one `http.Client`
and one transport across two goroutines. It starts `/slow`, waits 100 ms,
then starts `/fast`. Their lifetimes overlap, but they do not start simultaneously.
The delay encourages `/slow` to acquire the connection first; it is not a
synchronization guarantee.

The transport configuration keeps `MaxConnsPerHost: 1`,
`MaxIdleConnsPerHost: 1`, and `DisableKeepAlives: false` in both cases.
Requests use `https://localhost:9500/`. `InsecureSkipVerify: true` bypasses
certificate and hostname verification for this local self-signed certificate
experiment only.

Each measurement starts before the request log and `client.Get`, drains the
response body, closes it, and prints `resp.Proto`, status, and elapsed time.
Client duration therefore includes connection waiting, any connection/TLS setup,
server work, response transfer, and local scheduling/logging overhead.
Server duration covers only the handler's work and response writes.

### HTTP/1.1 Baseline and Protocol Verification

The baseline used the custom TLS transport without `ForceAttemptHTTP2: true`.
Adding a custom `TLSClientConfig` can suppress automatic HTTP/2 attempts;
the experiment verified the actual protocol through `resp.Proto`.
Setting `ForceAttemptHTTP2: true` enables an HTTP/2 attempt with this custom
configuration, but does not guarantee successful negotiation. See
[Go's Transport documentation](https://pkg.go.dev/net/http#Transport).

Sending plain HTTP to the HTTPS listener initially returned `400 Bad Request`.
Switching the client URL to `https://` resolved the mismatch. These failed
requests were excluded from the latency comparison.

HTTPS alone does not identify the HTTP version. In the successful HTTP/2 runs,
TLS ALPN negotiated HTTP/2, and the responses reported `HTTP/2.0`.

### Recorded Observations

The client and server ran on the same Windows machine over loopback.
The table records one HTTP/1.1 baseline run and two HTTP/2 runs; all requests
returned `200 OK`.

| Configuration     | Response protocol | `/slow` client duration | `/fast` client duration |
| ----------------- | ----------------- | ----------------------: | ----------------------: |
| HTTP/1.1 baseline | HTTP/1.1          |              2.032394 s |             1.9295976 s |
| HTTP/2, run 1     | HTTP/2.0          |             2.0296465 s |                1.619 ms |
| HTTP/2, run 2     | HTTP/2.0          |             2.0178681 s |               0.5093 ms |

For the HTTP/1.1 baseline, server logs reported:

```text
method=GET path=/slow duration=2.0134311s
method=GET path=/fast duration=0s
```

The reported `0s` does not establish that the handler performed zero work.
Clock resolution can limit measurements of such short intervals.

### Why Multiplexing Changes the Result

The HTTP/1.1 baseline timings are consistent with `/slow` acquiring the
available connection first. `/fast` started about 100 ms later and waited
for that connection to become available again. Its approximately 1.93-second
client duration is consistent with that wait, despite the very short `/fast`
handler duration.

With HTTP/2 enabled, `/fast` completed while `/slow` was still sleeping.
Each request/response exchange uses a separate stream, allowing frames for
different streams to share the connection. In this Go server, the two request
handlers ran concurrently. HTTP/2 provides multiplexing, while Go's server
provides handler concurrency. The sleep delays one handler without requiring
the other stream to wait for its response.

```text
HTTP/1.1, one available connection:
  /slow request -> wait ~2 s -> response -> /fast request -> response

HTTP/2, multiplexed streams:
  TCP connection
    stream A: /slow -> wait ~2 s -----------------> response
    stream B:      /fast -> response
```

Stream labels are illustrative; stream IDs and TCP connection identity were
not captured. The shared transport, one-connection limit, and successful
responses support the single-connection interpretation. Connection tracing
or a packet capture would provide direct confirmation.

### What the Experiment Proves

The results demonstrate that, in this setup, enabling negotiated HTTP/2 let
the fast request finish during the slow request's lifetime while keeping the
same configured connection limit. The slow request still took about two seconds.
Multiplexing reduced connection waiting for `/fast`; the intentional delay
in `/slow` remained unchanged.

### What It Does Not Prove

- HTTP/2 is always faster than HTTP/1.1.
- HTTP/1.1 cannot serve concurrent requests using multiple connections.
- HTTP/2 always uses one TCP connection in every application.
- Multiplexing increases available CPU or server processing capacity.
- HTTP/2 eliminates every form of head-of-line blocking.
- These few local runs establish latency percentiles or production performance.

The workload uses tiny responses and an artificial sleep. It does not measure
large response transfers, flow control, stream limits, header compression,
bandwidth contention, or packet loss. The recorded runs do not include the
actual Go toolchain version, ALPN traces, or connection IDs. Each client run
creates a fresh transport, so the first request can include connection setup;
the later request can use an already-established connection.

### The TCP-Level Limitation

HTTP/2 streams still share TCP's ordered byte stream. When a TCP segment is
lost, later bytes cannot be delivered to the application until the missing
bytes are recovered. That can delay data for several HTTP/2 streams sharing
the connection. Independent HTTP streams do not remove transport-level
head-of-line blocking. Packet loss was not introduced in this experiment.

### Reproducing the Experiment

Run from the repository root so the relative certificate paths resolve:

```sh
go run ./http2/fundamentals/server
```

In a second terminal:

```sh
go run ./http2/fundamentals/client -experiment=multiplexing
```

The client attempts HTTP/2 by default. Verify `protocol=HTTP/2.0` and
`status=200 OK` before interpreting latency. To repeat the HTTP/1.1 baseline:

```sh
go run ./http2/fundamentals/client -experiment=multiplexing -http2=false
```

Confirm `protocol=HTTP/1.1`. The `-http2` flag controls `ForceAttemptHTTP2`
for this custom TLS transport; the response records the actual outcome.
Each command starts a fresh client process.

### What I Learned

I learned to verify the protocol before interpreting performance. HTTPS and
HTTP/2 are separate properties, and transport configuration affects negotiation.
Comparing handler time with client time exposed connection waiting in the
HTTP/1.1 baseline. HTTP/2 multiplexing let independent requests progress
concurrently within the configured connection budget, while TCP's ordered
delivery remains a shared constraint.

---

## Day 2 — HTTP/2 Framing

### Goal

Inspect the protocol beneath Go's HTTP API: frame fields, stream IDs,
connection settings, and the flags that delimit headers and bodies.

### Capture Setup

The same HTTPS server and `/slow` + `/fast` client were used. The client now
sets `tls.Config.KeyLogWriter` to a temporary file named
`go-network-lab-tls-keys.log` and prints its full path. Wireshark uses that
file to decrypt the captured TLS traffic and display HTTP/2 frames.

Select this experiment with `-experiment=multiplexing` when running the client.

To repeat the inspection:

1. Start the server from the repository root.
2. Start a Wireshark capture on the loopback interface carrying localhost traffic.
3. Run the client and note the printed TLS key-log path.
4. Set Wireshark's TLS `(Pre)-Master-Secret log filename` preference to that file.
5. Filter for `http2` and expand the HTTP/2 frame entries in the details pane.

The client recreates the key-log file each run. Preserve the matching file
when retaining a capture for later inspection. It contains secrets that allow
decryption of the corresponding traffic; keep it out of Git.

The observations below were recorded during Wireshark inspection. Packet numbers
refer to that capture and will differ in another run.

### Reading the Frame Header

Each frame starts with nine bytes: payload length (3), type (1), flags (1),
and a reserved bit plus stream identifier (4). Length excludes the header.
Flags depend on frame type; Wireshark listing a flag field does not mean it
is set.

The capture associated `/slow` with stream 1 and `/fast` with stream 3.
Their response frames used the corresponding request stream IDs.
Connection-level SETTINGS and WINDOW_UPDATE frames appeared on stream 0.
WINDOW_UPDATE can also target an individual stream.

### Server SETTINGS — Packet 11

Packet 11 contained a server SETTINGS frame on stream 0 with a 30-byte
payload and no ACK flag. Five six-byte parameters account for the payload:
`5 × (2-byte identifier + 4-byte value) = 30 bytes`.
Including the frame header gives `30 + 9 = 39 bytes`.

| Setting | Recorded value | Interpretation of the server's advertisement |
| --- | ---: | --- |
| `SETTINGS_MAX_FRAME_SIZE` | 1,048,576 | Maximum frame payload the client is permitted to send to the server. |
| `SETTINGS_MAX_CONCURRENT_STREAMS` | 250 | Maximum number of concurrent streams the client may initiate. |
| `SETTINGS_MAX_HEADER_LIST_SIZE` | 10,488,896 | Advisory uncompressed field-section size limit. |
| `SETTINGS_HEADER_TABLE_SIZE` | 4,096 | Maximum HPACK dynamic-table size for the client's encoder. |
| `SETTINGS_INITIAL_WINDOW_SIZE` | 10,485,760 | Initial per-stream receive window; not the connection window. |

These settings describe this capture, not universal Go defaults.
Their direction matters: the server advertises constraints to the client.
The frame-size value is valid: the permitted range is 16,384–16,777,215 bytes.
An ACK acknowledges SETTINGS and carries no payload.

The packet's reported captured size was 125 bytes. That includes lower-layer
and TLS overhead, so it must not be equated with the 39-byte HTTP/2 frame.

### `/fast` Response — Packet 24

The expanded response details recorded two frames on stream 3:

| Frame | Type | Payload length | Flags | HTTP/2 frame size |
| --- | ---: | ---: | --- | ---: |
| HEADERS | 1 | 49 bytes | `0x04`: END_HEADERS | 58 bytes |
| DATA | 0 | 14 bytes | `0x01`: END_STREAM | 23 bytes |

Wireshark decoded `:status: 200` and
`content-type: text/plain; charset=utf-8` from the header block.
The HEADERS frame ended the header block without ending the server's sending
side of the stream. The following DATA frame carried `fast response\n`
and ended that side. Its body length matches the handler output:
`4 + 1 + 8 + 1 = 14 bytes`.

```text
Server -> client, stream 3
  HEADERS: 49-byte payload, END_HEADERS
    :status: 200
    content-type: text/plain; charset=utf-8
  DATA: 14-byte payload, END_STREAM
    fast response\n
```

END_HEADERS completes a header block; END_STREAM completes one sender's
side of a stream. The bodyless request HEADERS used `0x05`, combining both.
END_HEADERS is not a DATA flag.

### HPACK Observation

Wireshark represented `:status: 200` as an indexed header field with index 8.
That matches HPACK's static-table entry, rather than an uncompressed textual
status line.
The observed 49-byte HEADERS payload is an encoded block size, not the size
of the displayed header text. No compression ratio was measured.

### Interpretation and Limitations

The `/fast` response provides a concrete example of headers and body carried
in separate frames linked by one stream ID. Its final DATA flag identifies
completion independently of the other request's stream.

HTTP/2 frame boundaries do not have to align with TLS records or TCP packets.
One packet can contain multiple frames, and decoding a frame can require
reassembly across packets. The two frames displayed in packet 24 therefore
do not imply a universal packet layout.

The SETTINGS values are advertised limits, not measurements of server capacity
or achieved concurrency. The capture did not test 250 active streams, maximum
frame payloads, or flow-control exhaustion. Wireshark displayed flow-control
window information, but this exercise did not analyze its changes.

### What I Learned

I learned to separate decoded HTTP fields from frame metadata and to read the
actual flags value rather than list every flag shown by Wireshark. The capture
connected the application response to HEADERS and DATA, confirmed the 14-byte
body, and showed how stream IDs associate frames with an exchange. SETTINGS
provided a concrete starting point for investigating flow control and HPACK.

---

## Day 3 — HTTP/2 Streams and Flow Control

### Goal

Understand stream lifecycles, frame interleaving, TCP-level head-of-line blocking, and HTTP/2 flow control. Validate the concepts by inspecting decrypted HTTP/2 traffic in Wireshark.

### Stream Lifecycle and Multiplexing

HTTP/2 assigns a stream ID to each stream. In the observed client requests, `/slow` used stream 1 and `/fast` used stream 3. Client-initiated streams use odd IDs; server-initiated streams use even IDs.

Request and response frames belong to the same stream. Frames from different streams can be interleaved, allowing independent request/response exchanges to make progress without waiting for every other stream to finish.

`END_STREAM` indicates that the sender has finished sending on that stream. It does not necessarily mean the entire request/response exchange has finished. For example, a bodyless GET request can end with `END_STREAM` in its request HEADERS while the server still needs to send response headers and body data.

### HTTP/2 Multiplexing and TCP

HTTP/2 multiplexes streams over one TCP connection, but TCP still provides a single ordered byte stream.

If TCP bytes are lost, later bytes can be buffered but cannot be delivered to the application past the missing sequence range until TCP recovers the gap. Consequently, data for multiple HTTP/2 streams can be delayed by loss affecting the shared TCP connection.

HTTP/2 multiplexing reduces HTTP-level blocking in relevant scenarios; it does not eliminate TCP-level head-of-line blocking.

### Flow-Control Model

HTTP/2 flow control operates at two levels:

- **Stream-level window:** Limits DATA transmission on an individual stream.
- **Connection-level window:** Limits DATA transmission across the connection.

The sender must have sufficient credit in both windows to send a DATA frame. A zero stream window prevents further DATA on that stream, while a zero connection window prevents further DATA across streams on that connection.

A `WINDOW_UPDATE` frame adds credit to the window identified by its stream ID:

- Stream ID `0` updates the connection-level window.
- A nonzero stream ID updates that stream's window.

The increment is added to the current window; it does not replace the existing balance.

### Slow-Reader Experiment

With the server running, select the slow-reader experiment explicitly:

```sh
go run ./http2/fundamentals/client -experiment=slow-reader
```

This is also the default experiment. `-requests` defaults to 1 and controls
sequential runs using the same client and transport. For multiplexing, each
run sends one `/slow` + `/fast` pair. The slow-reader experiment requires HTTP/2.

The existing HTTPS server gained a `/large` endpoint that writes a 4 MiB response in 16 KiB chunks and flushes after each write.

The client requests `/large` over HTTP/2 and reads the response using a 4 KiB application buffer. After each successful `Read`, it sleeps for 100 ms to deliberately slow application-level consumption.

Observed client output:

- Protocol: `HTTP/2.0`
- Status: `200 OK`
- Content-Length: `4,194,304` bytes
- Bytes read: `4,194,304`
- Read duration: approximately 115 seconds

The response was received completely. The duration is an observation of this local experiment, not a network throughput benchmark. A single `Read` can return fewer than 4 KiB, so the sleep can occur more frequently than once per 4 KiB.

### Wireshark Observations

The capture used TLS key logging to decrypt the HTTPS traffic. The client requested `/large` on stream 1.

To inspect stream-1 DATA and WINDOW_UPDATE frames, I used this Wireshark display filter:

```text
http2.streamid == 1 && (http2.type == 0 || http2.type == 8)
```

Connection-level WINDOW_UPDATE frames use stream 0 and require separate inspection.

The following observations were recorded:

1. DATA frames appeared on stream 1 throughout the response transfer.
2. Both connection-level and stream-level `WINDOW_UPDATE` frames appeared during the transfer.
3. One observed connection-level update had a 4,096-byte increment. The connection window increased from `1,071,919,103` to `1,071,923,199` bytes.
4. In the same packet, the stream-1 window increased from `2,306,048` to `2,310,144` bytes, also a 4,096-byte increment.
5. An earlier connection-level update increased the connection window by `1,073,741,824` bytes (1 GiB).

These values describe the observed Go HTTP/2 implementation and this capture; they should not be treated as universal defaults.

### Interpretation and Limitations

The captured WINDOW_UPDATE increments show that the implementation replenished both connection-level and stream-level flow-control credit during the response transfer. They do not demonstrate a zero-window stall.

The application buffer size does not directly determine the HTTP/2 flow-control window size. Go manages internal buffering and flow-control updates, so the observed 4 KiB increments do not prove a one-to-one relationship with application `Read` calls.

This experiment did not establish that either flow-control window reached zero or that the sender stalled because of flow control. The large connection-level window made connection-window exhaustion unlikely during this 4 MiB transfer.

A packet gap alone would not prove flow-control blocking. Buffering, scheduling, and other implementation behavior can also affect observed timing.

### What I Learned

I learned to distinguish application reads from HTTP/2 flow-control credit, and stream-level limits from connection-level limits. I also verified how `WINDOW_UPDATE` frames identify the window being replenished.

Most importantly, I separated observed packet-level evidence from conclusions that the experiment did not establish. The next useful step would be a more controlled experiment that can demonstrate actual flow-control blocking.

---

## Day 4 — HTTP/2 Flow-Control Window Interaction

### Goal

Understand how stream-level and connection-level flow-control windows interact, how `WINDOW_UPDATE` restores sending credit, and why a stream can remain blocked even when the connection has available credit.

Day 3 introduced the flow-control mechanism and captured real `WINDOW_UPDATE` frames. This section consolidates the window interaction model and identifies what the existing experiment did and did not demonstrate.

### How the Two Windows Interact

To send DATA on a stream, the sender needs sufficient available credit in both the stream-level window and the connection-level window.

Conceptually, the amount of additional DATA that can be sent at a given moment is limited by the smaller of the two available windows.

| Connection-level credit | Stream-level credit | Result |
| --- | --- | --- |
| Available | Available | DATA can be sent, subject to both windows and other protocol constraints. |
| Available | Zero | DATA on that stream must wait for stream-level credit. Other streams may continue if their windows permit. |
| Zero | Available | DATA transmission across the connection must wait for connection-level credit. |
| Zero | Zero | DATA cannot be sent until the necessary credit is restored. |

These are conceptual cases, not states measured in the existing experiment.

Flow control applies to DATA frames. It does not directly restrict HEADERS frames in the same way.

### Understanding WINDOW_UPDATE

A `WINDOW_UPDATE` frame increases the sending credit associated with its stream ID.

- **Stream ID `0`:** Increases the connection-level window.
- **Nonzero stream ID:** Increases the window for that particular stream.
- **Increment:** Adds to the existing window; it does not replace the current balance.

For example, suppose Stream 1 has no remaining stream-level credit, while the connection-level window still has available credit.

A connection-level update such as:

```text
WINDOW_UPDATE
Stream ID: 0
Increment: 16384
```

adds 16,384 bytes of connection-level credit. It does not replenish Stream 1's individual window, so Stream 1 remains unable to send DATA until its stream-level credit is restored.

Conversely, replenishing Stream 1's window does not allow it to send DATA if the connection-level window has no available credit.

### Applying the Model to Multiple Streams

Consider one HTTP/2 connection carrying two streams:

```text
Connection
  Connection-level window: available

  Stream 1: large response
    Stream-level window: zero
    DATA transmission: blocked

  Stream 3: small response
    Stream-level window: available
    DATA transmission: permitted
```

In this scenario, Stream 1 can be blocked while Stream 3 continues to make progress.

This is an important distinction: stream-level flow control can constrain one stream independently, while connection-level flow control constrains DATA transmission across the connection.

The example assumes that Stream 3 has sufficient credit and that no other constraint prevents transmission.

### What Our Experiment Established

The Day 3 slow-reader experiment transferred a 4 MiB response over HTTP/2 while the client deliberately delayed application-level reads.

Wireshark showed both connection-level and stream-level `WINDOW_UPDATE` frames during the transfer. One observed packet contained a 4,096-byte increment for each window.

These observations establish that the implementation replenished flow-control credit during the transfer. They do not establish that a window reached zero or that transmission paused because of exhausted flow-control credit.

The connection-level window was large in this capture, making connection-window exhaustion unlikely during this particular 4 MiB transfer.

### Why the Application Read Buffer Is Not the Window Size

The client used a 4 KiB buffer when reading the response, but this does not mean the HTTP/2 stream window was 4 KiB.

The application read buffer, internal HTTP/2 buffering, and protocol flow-control windows are different mechanisms. The HTTP/2 implementation manages its own buffering and window updates.

Therefore, the observed 4,096-byte increments must not be interpreted as proof that each application `Read` caused exactly one corresponding `WINDOW_UPDATE`.

### What This Analysis Does Not Prove

- That either flow-control window reached zero.
- That the server stalled because of flow-control exhaustion.
- That every application read triggers a matching `WINDOW_UPDATE`.
- That a packet gap indicates flow-control blocking.
- That the observed window sizes and update increments are universal Go defaults.

A controlled experiment that deliberately exhausts a flow-control window would be needed to demonstrate an actual zero-window stall directly.

### What I Learned

I learned to reason about HTTP/2 flow control as two independent but jointly enforced limits. A stream needs both stream-level and connection-level credit to send DATA, and replenishing one window does not replenish the other.

I also learned to distinguish a protocol mechanism from evidence of that mechanism becoming a bottleneck. Our capture confirmed that both windows were updated, but it did not demonstrate flow-control exhaustion. That distinction matters when drawing conclusions from packet captures.

---

## Day 5 — HPACK Header Compression

### Goal

Understand how HPACK represents HTTP/2 header fields using static-table indices, literal representations, and dynamic-table references. Inspect actual request header blocks in Wireshark and compare their encoded sizes.

### Capture Setup

The existing HTTPS server and client were used to capture two requests to `/large` over HTTP/2.

To repeat both requests in one client process:

```sh
go run ./http2/fundamentals/client -experiment=slow-reader -requests=2
```

Both requests used the same TCP connection, with the first request on stream 1 and the second on stream 3. TLS key logging allowed Wireshark to decrypt the traffic and decode the HTTP/2 header blocks.

### First Request — Frame 14

The first request used a 41-byte HPACK header-block fragment. Wireshark decoded six header fields.

| Header field                     | Representation                                  | Index |
| -------------------------------- | ----------------------------------------------- | ----- |
| `:authority: localhost:9500`     | Literal with incremental indexing, indexed name | 1     |
| `:method: GET`                   | Indexed header field                            | 2     |
| `:path: /large`                  | Literal with incremental indexing, indexed name | 5     |
| `:scheme: https`                 | Indexed header field                            | 7     |
| `accept-encoding: gzip`          | Literal with incremental indexing, indexed name | 16    |
| `user-agent: Go-http-client/2.0` | Literal with incremental indexing, indexed name | 58    |

The indexed fields for `:method` and `:scheme` refer to static-table entries. The other fields use literal representations with incremental indexing, allowing them to be added to the dynamic table.

### Second Request — Frame 3094

The second request used a 9-byte HPACK header-block fragment. Wireshark decoded seven header-block entries, including a header table size update.

| Header field                     | Representation       | Index |
| -------------------------------- | -------------------- | ----- |
| `:authority: localhost:9500`     | Indexed header field | 65    |
| `:method: GET`                   | Indexed header field | 2     |
| `:path: /large`                  | Indexed header field | 64    |
| `:scheme: https`                 | Indexed header field | 7     |
| `accept-encoding: gzip`          | Indexed header field | 63    |
| `user-agent: Go-http-client/2.0` | Indexed header field | 62    |

Indices 2 and 7 refer to static-table entries. Indices 62–65 refer to dynamic-table entries.

The second request therefore provides direct evidence that the connection's dynamic-table state was reused. Instead of encoding the repeated field values literally, the header block references existing entries.

The header block also contained a dynamic-table size update of 4096 bytes.

### Encoded Size Comparison

| Observation                   | First request                  | Second request |
| ----------------------------- | ------------------------------ | -------------- |
| Encoded header-block fragment | 41 bytes                       | 9 bytes        |
| Decoded header fields         | 6                              | 6              |
| Dynamic-table references      | None reported for these fields | Four reported  |

The encoded header-block fragment decreased by 32 bytes, from 41 to 9 bytes, approximately a 78% reduction for these two observed blocks.

This is a comparison of two specific encoded header blocks, not a general HPACK compression ratio or a measurement of total network traffic.

### Interpretation and Limitations

The capture demonstrates the role of both static and dynamic tables. Static entries efficiently represent common fields, while dynamic entries allow repeated field values to be referenced within the same connection's compression context.

The decoded header length reported by Wireshark is not the same as the encoded header-block size. Encoded blocks contain compact representations that the decoder reconstructs into header fields.

The experiment used repeated requests to one local endpoint. It did not measure compression across a broad range of headers, connections, or workloads. The observed sizes should not be generalized into a universal compression ratio.

### What I Learned

I learned to distinguish a fully indexed header field from a literal field that uses an indexed name. I also verified that incremental indexing can establish dynamic-table state for later requests.

The second capture demonstrated actual dynamic-table reuse: repeated fields were represented by dynamic-table indices, reducing the encoded header-block fragment from 41 bytes to 9 bytes in this example.

This showed why HPACK's stateful compression can reduce repeated header overhead without removing the logical HTTP headers from subsequent requests.

---

## Day 6 — HTTP/2 Implementation and Final Synthesis

### Goal

Consolidate the HTTP/2 concepts explored in this phase and connect protocol-level observations to Go's standard HTTP client and server implementation.

### Go's HTTP/2 Implementation

The experiments used Go's `net/http` package rather than implementing an HTTP/2 stack from scratch.

The server used `http.ListenAndServeTLS`, while the client used `http.Client` and `http.Transport`. The client configured `ForceAttemptHTTP2: true` alongside a custom `TLSClientConfig`.

This configuration enables Go's transport to attempt HTTP/2 under the custom TLS configuration. It does not guarantee negotiation. The experiment verified the negotiated protocol through `resp.Proto`, which reported `HTTP/2.0` in the successful runs.

The implementation handled protocol details such as framing, stream management, flow control, and HPACK. The experiments used Wireshark to inspect those mechanisms rather than reimplementing them.

### Concepts Consolidated

**Multiplexing and streams**

HTTP/2 allows multiple request/response exchanges to share one connection using independent streams. The `/slow` and `/fast` experiment demonstrated that the fast response could complete while the slow handler was still running.

**Binary framing**

HTTP/2 carries headers, data, settings, and control information in frames. Frame type, flags, stream ID, and payload length help identify each frame's role.

**Stream lifecycle**

Request and response frames use the same stream ID. `END_HEADERS` marks the end of a header block, while `END_STREAM` marks the end of one endpoint's sending side of a stream.

**TCP-level head-of-line blocking**

HTTP/2 multiplexing does not remove TCP's ordered delivery constraint. Loss affecting the shared TCP byte stream can delay data belonging to multiple HTTP/2 streams.

**Flow control**

DATA transmission requires available credit in both the stream-level and connection-level windows. `WINDOW_UPDATE` increases the credit associated with the identified window.

The slow-reader experiment captured both types of updates, but did not demonstrate a zero-window stall.

**HPACK**

HPACK represents header fields using static-table indices, literal representations, and dynamic-table references. The repeated-request experiment showed dynamic-table reuse in the second request.

The encoded header-block fragment decreased from 41 bytes to 9 bytes in the observed requests. This is a result from the specific capture, not a general compression ratio.

### Evidence and Limitations

The phase combined application-level experiments with decrypted Wireshark captures.

The observations were made using a local Go server and client over loopback, with a self-signed certificate and a synthetic workload. The experiments were designed to understand protocol behavior, not to establish production performance or capacity.

The work did not implement a complete HTTP/2 stack, HPACK encoder, or custom flow-control mechanism. It also did not comprehensively test packet loss, maximum concurrent streams, or flow-control exhaustion.

### Final Learning Outcome

I learned to connect Go's high-level HTTP APIs with the protocol behavior visible in HTTP/2 frames. I practiced forming hypotheses, running controlled experiments, inspecting evidence, interpreting results, and documenting limitations.

The most important outcome was learning to distinguish what an experiment directly demonstrates from what the protocol theory predicts but the experiment has not verified.
