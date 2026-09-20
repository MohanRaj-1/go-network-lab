# TLS Concepts and Experiments

This document records the concepts, experiments, and observations from implementing and studying TLS in Go Network Lab.

The goal is not to implement cryptographic primitives or TLS from scratch. Instead, this experiment focuses on understanding how TLS protects application traffic, how the handshake establishes security parameters, how TLS operates over TCP, and how Go's `crypto/tls` package provides the implementation.

## 1. Learning Objectives

- Understand why TLS is required for secure application communication.
- Understand the difference between symmetric and asymmetric cryptography.
- Understand the role of certificates and certificate authorities.
- Understand the major stages of the TLS handshake.
- Understand how TLS 1.3 protects application data.
- Understand the relationship between TCP, TLS records, and application data.
- Integrate TLS with an HTTP server built directly over TCP.
- Observe TLS behavior using Go, curl, and Wireshark.
- Understand the responsibilities delegated to the standard library.

## 2. Why TLS Is Necessary

TCP provides a reliable, ordered byte stream between two endpoints. However, TCP does not provide:

- Confidentiality
- Integrity protection against an attacker
- Authentication of the remote server
- Protection against man-in-the-middle attacks

A plain HTTP request can be observed or modified by an attacker who can access the communication path.

TLS is a security protocol that operates above the transport layer and below the application protocol.

For HTTPS, the protocol stack is:

    HTTP
      |
    TLS
      |
    TCP
      |
    IP

TLS provides the security layer while allowing HTTP to continue using a byte-stream connection.

### TLS security properties

#### Confidentiality

Application data is encrypted so that an observer cannot normally read its contents.

#### Integrity

TLS detects unauthorized modification of protected records.

#### Authentication

The server can prove its identity using a certificate chain trusted by the client.

Authentication depends on certificate validation and the client's trust configuration. Encryption alone does not prove that the client is communicating with the intended server.

## 3. Cryptographic Foundations

### 3.1 Symmetric cryptography

Symmetric cryptography uses the same secret key to encrypt and decrypt data.

It is suitable for protecting large amounts of application data because it is computationally efficient.

TLS uses symmetric authenticated encryption for application traffic after the required keys have been established.

### 3.2 Public-key cryptography

Public-key cryptography uses a public key and a corresponding private key.

The private key must remain secret. The public key can be distributed.

Public-key cryptography supports operations such as authentication and key establishment, depending on the algorithm and protocol.

It is generally more computationally expensive than symmetric encryption, so TLS does not use public-key operations to encrypt all application data.

### 3.3 Hybrid design

TLS combines different cryptographic techniques:

1. The handshake negotiates security parameters.
2. The server authenticates itself using its certificate and private key.
3. The handshake establishes shared key material.
4. Symmetric authenticated encryption protects application data.

This design combines authentication and key establishment with efficient application-data encryption.

## 4. Certificates and PKI

A TLS certificate binds an identity to a public key.

A certificate may contain information such as:

- Subject information
- Subject Alternative Names (SANs)
- Public key
- Issuer
- Validity period
- Digital signature from the issuing authority

### Certificate authority

A certificate authority (CA) signs certificates and forms part of a trust chain.

A client validates a certificate according to its trust configuration and the certificate's properties.

Validation can include:

- Signature-chain verification
- Validity-period checks
- Hostname verification
- Key-usage constraints
- Subject Alternative Name matching

### Local learning certificate

Our local certificate is self-signed and intended for local experimentation.

It includes localhost and 127.0.0.1 as names or addresses for the experiment.

The certificate is not automatically trusted by every client. Our Go integration test explicitly configures a certificate pool containing the test certificate.

This is different from deploying a publicly trusted certificate in a production environment.

### SAN and hostname validation

Modern hostname verification uses the Subject Alternative Name extension.

A certificate for `localhost` should not automatically be assumed to be valid for an unrelated hostname.

The client must validate that the requested server name matches an appropriate identity in the certificate.

## 5. TLS Handshake

### Authentication during the handshake

In a typical TLS 1.3 server-authenticated handshake:

1. The client and server exchange handshake messages and establish
   the parameters needed for the connection.
2. The server presents its certificate chain.
3. The server provides a signature proving possession of the private
   key associated with its certificate.
4. The client validates the certificate and the server's signature
   according to its trust configuration.
5. The handshake establishes shared secret material using ephemeral
   key agreement.
6. Traffic keys are derived from the handshake secrets.

The certificate and the ephemeral key agreement serve different purposes:

- The certificate and signature authenticate the server.
- The key agreement establishes shared secret material.
- The derived traffic keys protect application data.

### Key establishment and authentication

These are related but distinct concepts:

- Key establishment allows both parties to derive shared secret material.
- Authentication helps the client verify the identity of the server.

A connection can use encryption without authenticating the expected identity correctly if certificate validation is disabled or incorrectly configured.

### Key derivation

The handshake derives traffic keys from the established secret material using a key schedule.

TLS 1.3 uses HKDF-based key derivation.

The resulting traffic keys are used by authenticated encryption to protect records.

## 6. TLS Records and TCP

TCP transports an ordered byte stream. It does not preserve application message boundaries.

TLS adds its own record layer above TCP.

The conceptual flow is:

    Application data
          |
    TLS record layer
          |
    TCP byte stream

A single application write does not necessarily correspond to one TCP segment or one observable network packet.

Similarly:

- One TLS record may be split across multiple TCP segments.
- Multiple TLS records may be carried in TCP segments.
- A large application write may produce multiple TLS records.
- TCP segmentation depends on transport and network conditions.

### Important distinction

The following are different concepts:

| Concept           | Meaning                                    |
| ----------------- | ------------------------------------------ |
| Application write | Data supplied by the application to TLS    |
| TLS record        | Unit of processing in the TLS record layer |
| TCP segment       | Transport-layer transmission unit          |
| Captured frame    | Unit displayed by a packet-capture tool    |

A Wireshark frame is not necessarily equivalent to a TLS record or an application write.

## 7. Go TLS Implementation

The experiment uses Go's `crypto/tls` package.

We deliberately do not implement the following ourselves:

- AES
- RSA
- ECDSA
- Diffie-Hellman primitives
- HKDF
- AEAD algorithms
- TLS record protection
- Certificate validation
- TLS handshake state machines

Implementing these components from scratch would introduce unnecessary security risks and would move the project away from its networking-learning objective.

The focus is understanding the protocol and correctly integrating a well-tested standard-library implementation.

The server performs the following operations:

1. Load the certificate and private key.
2. Configure the TLS server.
3. Accept a TCP connection.
4. Wrap the connection using `tls.Server`.
5. Perform the TLS handshake.
6. Inspect the negotiated connection state.
7. Pass the established TLS connection to the raw HTTP server.

The HTTP parser operates on the `net.Conn` interface and does not need to know whether the underlying connection is plain TCP or TLS.

## 8. Experiment: Inspecting TLS Connection State

### Setup

After completing the TLS handshake, the client and server inspected `tls.ConnectionState`. The server logged the negotiated TLS version and cipher suite. The HTTPS server also logged the requested server name and ALPN result.

### Observation

The observed connection included:

- TLS version: TLS 1.3
- Cipher suite: TLS_AES_128_GCM_SHA256
- Server name: localhost
- ALPN: empty
- Handshake complete: true

The negotiated values are determined during the handshake and depend on the client and server configuration.

An empty ALPN value is expected in this experiment because we did not configure an application protocol such as HTTP/2 through ALPN.

### Conclusion

The values in `tls.ConnectionState` describe the session that the client and server actually negotiated. They are runtime results rather than assumptions based only on the local configuration. A completed handshake produced TLS 1.3 with `TLS_AES_128_GCM_SHA256`, while the empty ALPN result showed that no application protocol was negotiated through ALPN.

## 9. Experiment: TLS Key Logging and Wireshark

### Setup

The Go TLS client configured `tls.Config.KeyLogWriter` to write session secrets to `tls-keys.log`. The corresponding connection traffic was captured in Wireshark, and the key-log file was supplied to Wireshark for TLS decryption.

The repository ignores `tls-keys.log` and `*.keys.log` so this debugging material is not added to version control.

### Observation

Before Wireshark used the matching key material, the capture exposed TLS records but not the plaintext application content. With the session traffic and matching key-log entries available, Wireshark could decrypt and dissect that TLS session.

The capture showed TLS 1.3 application-data records. The outer record type appeared as `application_data`, including records whose protected contents were not visible without decryption. Captured record and frame lengths did not provide a direct measurement of the original application `Write` call.

### Conclusion

The key log contains session-specific key material that a compatible
packet-analysis tool can use to decrypt the corresponding captured
TLS session.

A key log is useful only when the relevant session traffic and
matching key material are available. It does not provide a general
ability to decrypt unrelated TLS connections.

Key logging changes what the analysis tool can observe; it does not disable TLS on the network. The transmitted traffic remains TLS-protected, but possession of both the capture and its matching secrets permits decryption.

### Security considerations

This capability must be handled carefully:

- Key logs expose material needed to decrypt captured traffic.
- Key logging should be restricted to controlled learning or debugging environments.
- Key logs must not be committed to the repository.
- Key logging should not be enabled casually in production.

## 10. Experiment: Large Application Write

### Setup

The server created a known 10,000-byte payload and supplied it to TLS with one application-level call:

```go
message := strings.Repeat("A", 10_000)
n, err := tlsConn.Write([]byte(message))
```

The server logged the number of application bytes accepted by `Write`, and the traffic was captured in Wireshark.

### Observation

The application log reported a 10,000-byte write. The packet capture contained multiple captured frames rather than one transmission corresponding to the complete application write.

The capture boundaries did not match the boundary of the single Go `Write` call.

### Conclusion

An application write does not define a fixed one-to-one mapping to TLS records, TCP segments, or captured frames. A large application write can be processed into multiple TLS records and transported through multiple TCP segments or frames.

The exact number and layout of records or frames depend on implementation behavior and transport conditions. The observation should not be generalized into a fixed relationship between application writes, TLS records, and TCP segments.

The capture demonstrates that the boundaries of a Go application
Write call are not preserved as network-visible message boundaries.
It does not, by itself, establish the exact number of TLS records
generated by that call.

## 11. Experiment: Plain HTTP Sent to a TLS Port

### Setup

A plain HTTP request was sent to a port expecting TLS.

### Observation

The server reported an error similar to:

```text
tls: first record does not look like a TLS handshake
```

### Conclusion

This occurs because the server expects the beginning of a TLS handshake, but receives bytes that do not represent a valid TLS record for the expected protocol.

A TLS endpoint must complete the TLS handshake before its application protocol can be processed. It cannot treat arbitrary plaintext HTTP bytes as an already-established HTTP connection.

## 12. Experiment: Graceful Connection Closure

### Setup

The client completed the TLS handshake and then closed its `tls.Conn` normally. After its own successful handshake, the server waited in `tlsConn.Read` for application data or connection closure. The handler used deferred connection cleanup.

### Observation

The client logged that the TLS connection closed gracefully. The server's read returned zero bytes with `io.EOF`, which the server logged as a peer closure rather than a TLS read failure.

```text
TLS connection closed by peer: bytes=0
```

### Conclusion

For this experiment, `io.EOF` after a clean peer close represented normal end-of-stream behavior rather than an application failure. The handler should return and release the connection when it observes that condition.

Normal closure, TLS protocol errors, and transport errors are distinct outcomes. They must be classified separately from HTTP parsing errors, which occur only after TLS has provided application bytes.

## 13. HTTPS Integration

### Architecture

The raw HTTP implementation was extracted into the `internal/rawhttp` package.

Both HTTP and HTTPS servers reuse the same HTTP connection handler:

- Plain HTTP passes a TCP connection directly to the HTTP handler.
- HTTPS completes a TLS handshake and passes the TLS connection to the same handler.

This avoids duplicating:

- HTTP request parsing
- HTTP framing
- Routing
- Response generation
- Persistent-connection handling

The HTTP implementation depends on the `net.Conn` interface, allowing the same application-layer logic to operate over different connection types.

The request flow is:

    TCP accept
        |
    TLS handshake
        |
    Established TLS connection
        |
    Raw HTTP parser
        |
    Request routing
        |
    HTTP response
        |
    TLS-protected response bytes

### Implementation

The HTTPS listener accepts a TCP connection and wraps it with `tls.Server`. It explicitly completes the handshake before calling:

```go
rawhttp.HandleConnection(tlsConn)
```

Reads from `tlsConn` return decrypted HTTP bytes. Writes made by the raw HTTP handler are encrypted by TLS before being carried over TCP. TLS and HTTP errors remain owned by their respective layers, and deferred closes release connections on successful completion and early returns.

### Outcome

The existing routes, request framing, validation, response writing, and persistent-connection behavior work over HTTPS without adding TLS knowledge to the HTTP package. The integration changed the connection supplied to the handler rather than duplicating the HTTP implementation.

## 14. Integration Tests

### Test setup

The HTTPS integration tests use an in-memory connection arrangement with `net.Pipe`.

The server side uses the real HTTPS connection handler. The client side is wrapped with `tls.Client`, trusts the local test certificate through an explicit certificate pool, and verifies the server name `localhost`. Certificate verification is not disabled.

One test sends `GET /` with `Connection: close`. The other sends `POST /echo` with a Content-Length-framed body. Both tests wait for the server handler to finish within two seconds.

### Verification

The tests verify:

- TLS handshake completion
- Certificate configuration
- GET request handling
- POST request handling
- HTTP response status
- Response body
- Connection completion within a timeout

The POST test also verifies that the HTTP body is received and echoed through the HTTPS connection.

Together, these checks verify that the client and server can negotiate TLS, exchange encrypted request and response bytes, reuse the raw HTTP parser and routes, and complete the tested connection lifecycle.

The tests verify the behavior of the configured TLS and HTTP components together. They do not independently verify the cryptographic correctness of the TLS implementation, which is provided by Go's `crypto/tls` package.

### Limitations

Using an in-memory connection makes the test deterministic and avoids requiring a real listening port for the integration test.

The tests do not exercise the operating system's TCP stack, a real listening socket, DNS, external-client behavior, packet segmentation, or browser trust configuration. They also cover only the implemented GET and POST success paths; they are not a complete TLS, HTTP, or failure-mode test suite.

They do not replace end-to-end testing with curl. The two approaches serve different purposes:

- Integration tests verify behavior programmatically.
- curl-based testing verifies that an external client can communicate with the server.

The `net.Pipe` tests verify the TLS and HTTP handler interaction
without exercising real network transport behavior. They should
therefore be complemented by tests using a real listening socket
and an external client, such as curl.

## 15. Lessons Learned

- TCP provides a byte stream, not message boundaries.
- TLS protects application traffic carried over TCP.
- Encryption and authentication are distinct security properties.
- Certificates authenticate identities through a trust model.
- The certificate's public key is not the symmetric traffic key.
- TLS records and TCP segments are different protocol concepts.
- Packet-capture frames cannot be treated as application writes.
- HTTP parsing can be reused over TLS when the implementation depends on `net.Conn`.
- The standard library should provide cryptographic and TLS protocol primitives.
- Tests should verify both successful communication and failure behavior.
- Debugging facilities such as TLS key logging require careful handling.

## 16. Scope and Limitations

This experiment is intended for learning and protocol understanding.

It is not a complete production HTTPS server implementation.

The project does not attempt to implement:

- A complete HTTP specification
- A complete TLS implementation
- Certificate issuance infrastructure
- Production certificate rotation
- HTTP/2
- HTTP/3
- Production-grade observability
- Full TLS policy hardening
- Comprehensive performance benchmarking

The TLS cryptographic implementation is delegated to Go's `crypto/tls` package.

The raw HTTP implementation intentionally supports a limited subset of HTTP behavior for learning purposes.
