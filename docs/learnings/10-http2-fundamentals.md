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
