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
go run ./http2/fundamentals/client
```

The current client enables `ForceAttemptHTTP2: true`. Verify `protocol=HTTP/2.0`
and `status=200 OK` before interpreting latency. To repeat the HTTP/1.1
baseline with this transport, temporarily set `ForceAttemptHTTP2: false`,
run a fresh client process, and confirm `protocol=HTTP/1.1`. Restore `true`
afterward. If the protocol differs, investigate configuration before comparing
timings; the flag controls an attempt, while the response records the outcome.

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
