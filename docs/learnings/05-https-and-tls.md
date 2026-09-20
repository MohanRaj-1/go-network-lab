# HTTPS and TLS Integration

## Goal

After building HTTP/1.1 parsing directly on TCP, I added HTTPS without changing the HTTP parser. The goal was to understand where TLS belongs in the connection stack and how an encrypted connection becomes the same byte stream that the existing HTTP code already knows how to process.

The HTTPS server listens on `:8443`. It uses the certificate and private key from the earlier TLS experiments, requires TLS 1.2 or newer, performs the handshake explicitly, and then reuses `rawhttp.HandleConnection` for HTTP request handling.

This is a learning server. It demonstrates the boundary between TCP, TLS, and HTTP rather than providing all the configuration, timeouts, certificate management, and operational controls needed by a production server.

## 1. Where TLS Fits

TLS was integrated beneath the HTTP layer because each layer has a different responsibility:

```text
HTTP request and response messages
              |
       rawhttp parser
              |
   decrypted TLS byte stream
              |
       TLS records and handshake
              |
          TCP byte stream
```

TCP carries ordered bytes but does not protect them. TLS uses that TCP stream to negotiate encryption and integrity protection, then exposes a new stream containing decrypted application bytes. HTTP defines the meaning and boundaries of those application bytes.

The raw HTTP package should not need to understand certificates, cipher suites, TLS records, or handshakes. It receives a `net.Conn` and reads HTTP bytes from it. This separation lets the same HTTP implementation work with either a plain TCP connection or a TLS-wrapped connection.

## 2. Building the HTTPS Listener

The server first loads its certificate and corresponding private key:

```go
cert, err := tls.LoadX509KeyPair(
    "tcp/tls/basic/certs/server.crt",
    "tcp/tls/basic/certs/server.key",
)
```

It then creates the server TLS configuration:

```go
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS12,
}
```

The certificate lets the server prove its identity during the handshake. `MinVersion` prevents negotiation below TLS 1.2.

The listener itself remains a plain TCP listener:

```go
listener, err := net.Listen("tcp", ":8443")
```

Each accepted value is therefore an ordinary TCP connection. The server starts a goroutine and passes that connection together with the shared, read-only TLS configuration:

```go
go handleConnection(conn, tlsConfig)
```

## 3. Wrapping TCP with `tls.Server`

The connection handler turns the accepted TCP connection into a server-side TLS connection:

```go
tlsConn := tls.Server(conn, tlsConfig)
```

`tls.Server` does not create another network connection. It wraps the existing `net.Conn`. Reads and writes on `tlsConn` use the underlying TCP connection, but add TLS behavior:

- Reads consume TLS records from TCP, authenticate and decrypt them, and return plaintext.
- Writes accept plaintext, encrypt it into TLS records, and send those records through TCP.
- Handshake messages are exchanged before application data is processed.

Both `net.Conn` and `*tls.Conn` satisfy the same `net.Conn` interface. This is the key composition point: code that expects a `net.Conn` can work with the TLS wrapper without knowing how encryption is implemented.

## 4. Handshake Before HTTP

The HTTPS handler performs an explicit handshake:

```go
if err := establishTLS(tlsConn); err != nil {
    fmt.Println("TLS handshake:", err)
    return
}
```

Only after it succeeds does the server invoke the HTTP handler:

```go
rawhttp.HandleConnection(tlsConn)
```

This ordering matters. Before a successful handshake, the peer has not completed TLS version and cipher negotiation, the server certificate has not been presented and validated by the client, and protected application traffic is not ready. Passing a failed TLS connection into the HTTP parser could make TLS handshake bytes look like malformed HTTP.

Go can start a handshake automatically on the first TLS read or write. Calling `Handshake` explicitly gives the HTTPS entry point a clear boundary: TLS failures are reported as TLS failures, and HTTP processing begins only for an established secure connection.

After the handshake, the server logs the negotiated TLS version, cipher suite, requested server name, and ALPN result from `ConnectionState`. The HTTP implementation itself supports the experiment's HTTP/1.1 subset.

## 5. Reusing the Raw HTTP Handler

The reusable HTTP entry point is:

```go
func HandleConnection(conn net.Conn)
```

The plain HTTP server passes an accepted TCP connection to it:

```go
rawhttp.HandleConnection(conn)
```

The HTTPS server passes the TLS wrapper instead:

```go
rawhttp.HandleConnection(tlsConn)
```

Inside `rawhttp`, the same request reader, validation, body framing, routing, persistent-connection loop, and response writer are used in both cases. A call to `Read` returns plaintext HTTP bytes because `tlsConn` decrypts the TLS records first. A call to `Write` supplies plaintext HTTP response bytes to TLS, which encrypts them before they reach TCP.

This reuse confirms that HTTPS is HTTP carried through TLS. HTTPS did not require a second HTTP parser or separate route implementations.

## 6. GET and POST Integration Tests

The HTTPS integration tests use `net.Pipe` to create two connected in-memory endpoints. The server endpoint is passed to the real HTTPS connection handler. The client endpoint is wrapped with `tls.Client`:

```text
HTTP test request
       |
  tls.Client
       |
   net.Pipe
       |
  tls.Server
       |
rawhttp.HandleConnection
```

The client trust pool contains `server.crt`, and its expected server name is `localhost`:

```go
clientTLSConfig := &tls.Config{
    RootCAs:    certificatePool(t, certPath),
    ServerName: "localhost",
    MinVersion: tls.VersionTLS12,
}
```

This means the tests do real certificate-chain and hostname verification. They do not bypass verification with `InsecureSkipVerify`.

`TestHTTPSConnection` completes a client and server TLS handshake, sends this request through the encrypted connection:

```text
GET / HTTP/1.1
Host: localhost
Connection: close
```

It verifies that the decrypted response contains `HTTP/1.1 200 OK` and `Hello World!`.

`TestHTTPSPostEcho` sends a `POST /echo` request with an exact `Content-Length` and the body `Hello from HTTPS integration test`. It verifies both a 200 response and the echoed body. This test crosses every implemented layer: HTTP body framing, HTTP routing, TLS encryption and decryption, and connection closure.

Both tests wait for the server goroutine to finish, with a two-second timeout. This checks that `Connection: close` is honored and that the handler does not remain blocked after completing the response.

Run the HTTPS tests from the project root:

```sh
go test -v ./tcp/https/server
```

## 7. TLS Errors and Connection Cleanup

Errors are handled at the layer where they occur.

Startup errors from loading the certificate or opening the listener are wrapped and returned from `run`. An accept failure is logged, and the accept loop continues so one failed accept does not intentionally stop the server.

A handshake error is logged by the HTTPS handler and prevents the connection from reaching `rawhttp.HandleConnection`. Examples include a client sending plaintext HTTP to port 8443, incompatible TLS settings, or malformed handshake data. Client-side trust and hostname failures are generally reported by the client; the server may observe the peer terminating the handshake.

After a successful handshake, HTTP parse and write errors remain the responsibility of `rawhttp.HandleConnection`. TLS decrypts data but does not decide whether a request line, Host header, Content-Length, or chunked body is valid HTTP.

The HTTPS handler defers closure of the accepted connection and the TLS wrapper:

```go
defer conn.Close()

tlsConn := tls.Server(conn, tlsConfig)
defer tlsConn.Close()
```

`rawhttp.HandleConnection` also closes the `net.Conn` it owns when its request loop returns. With HTTPS, that interface contains `tlsConn`, so the HTTP handler closes the TLS connection. The surrounding deferred closes ensure cleanup on handshake failures and other early returns as well. Repeated close attempts are harmless for this implementation, though a larger design could assign connection ownership to one layer more explicitly.

Closing the TLS connection attempts to send the TLS closure notification before the underlying TCP connection is released. A peer can still disappear abruptly, so cleanup code must not assume that every connection ends with a graceful TLS shutdown.

## 8. Encryption and Parsing Are Different Jobs

TLS encryption and HTTP parsing solve separate problems.

TLS provides:

- confidentiality, so observers cannot read the HTTP plaintext;
- integrity, so modification of protected records is detected;
- server authentication through certificate verification;
- negotiated protocol versions and cipher suites.

The raw HTTP layer provides:

- request-line and header parsing;
- Host validation;
- Content-Length and chunked body framing;
- routing for `/` and `/echo`;
- HTTP status lines, headers, and response bodies;
- HTTP connection persistence and closure rules.

Encryption does not make malformed HTTP valid, and correct HTTP parsing does not encrypt traffic. The layers meet at the plaintext `net.Conn` abstraction exposed by `*tls.Conn`.

## 9. Running the HTTPS Server

The certificate paths in the server are relative to the repository root, so run it from there:

```sh
go run ./tcp/https/server
```

The server listens at `https://localhost:8443`.

With curl, explicitly trust the learning certificate:

```sh
curl.exe --cacert tcp/tls/basic/certs/server.crt https://localhost:8443/
curl.exe --cacert tcp/tls/basic/certs/server.crt -X POST https://localhost:8443/echo -d "hello HTTPS"
```

The first request should return `Hello World!`. The second should return `hello HTTPS`.

## 10. What I Learned

The main result was that adding HTTPS did not require changing the HTTP protocol implementation. The TLS wrapper translated between encrypted TLS records and a plaintext byte stream, while `rawhttp.HandleConnection` continued to parse and write HTTP exactly as it did for plain TCP.

The explicit handshake made the boundary visible. A connection must first become an established TLS session; only then is it meaningful to interpret its application bytes as HTTP. It also kept error reporting clear by preventing TLS negotiation failures from being mistaken for HTTP parsing failures.

The GET and POST integration tests showed that the layers work together in both directions. Requests were encrypted by the client, decrypted and parsed by the server, routed by the existing HTTP code, encrypted again as responses, and finally decrypted by the client. Certificate trust and hostname verification were part of the test rather than disabled for convenience.
