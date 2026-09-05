# Application Protocol over TCP

## 1. Objective

In [TCP and Message Framing](01-tcp-and-framing.md), I implemented a way to send complete messages over a TCP byte stream. In this phase, I built an application protocol on top of that framing layer.

The goal was to give those messages meaning: define requests and responses, validate incoming requests, perform supported operations, and decide how errors affect a connection.

I implemented a shared Go protocol package, a TCP server, an experiment client, and automated tests.

## 2. From Framing to an Application Protocol

Framing tells the receiver where a message ends. An application protocol defines how to interpret that message and respond to it.

My implementation has three layers:

```text
TCP byte stream
    ↓
Length-prefixed frames
    ↓
JSON requests and responses
```

Each frame contains one JSON payload:

```text
[4-byte unsigned big-endian payload length][JSON payload]
```

The length counts payload bytes only; it excludes the four-byte header. The maximum payload size is 1 MiB (1,048,576 bytes).

JSON makes the request and response fields easy to inspect. The length prefix still provides message boundaries, so the receiver does not depend on a single TCP read returning an entire JSON message.

## 3. Protocol Design

### 3.1 Request Format

Requests use the following fields:

| Field     | JSON type | Rule                                                                                    |
| --------- | --------- | --------------------------------------------------------------------------------------- |
| `version` | Integer   | Must be `1`, the supported protocol version.                                            |
| `id`      | Integer   | Must be positive and is copied into the response to identify the corresponding request. |
| `type`    | String    | Must be `PING`, `ECHO`, or `TIME`; values are case-sensitive.                           |
| `message` | String    | Required and nonempty for `ECHO`; not required for `PING` or `TIME`.                    |

The server checks that an ID is positive but does not enforce uniqueness. Requests on a connection are processed sequentially.

### 3.2 Response Format

| Field     | JSON type | Purpose                                                              |
| --------- | --------- | -------------------------------------------------------------------- |
| `version` | Integer   | Server protocol version, currently `1`.                              |
| `id`      | Integer   | ID copied from the decoded request, including for validation errors. |
| `type`    | String    | `PONG`, `ECHO`, `TIME`, or `ERROR`.                                  |
| `message` | String    | Echoed text or an error description.                                 |
| `value`   | String    | Server time for a `TIME` response.                                   |

The Go structures use `omitempty` for `message` and `value`, so empty values are omitted when encoding JSON.

An error response for an invalid ID still carries that invalid ID because the server constructs the error response using the ID from the decoded request.

### 3.3 Supported Operations

The protocol currently supports three request types:

#### `PING`

Used to check whether the server is responding.

```json
{
  "version": 1,
  "id": 1,
  "type": "PING"
}
```

The server responds with `PONG`.

#### `ECHO`

Returns the message provided by the client.

```json
{
  "version": 1,
  "id": 2,
  "type": "ECHO",
  "message": "hello"
}
```

The `message` field is required for an `ECHO` request.

#### `TIME`

Requests the server's current UTC time.

```json
{
  "version": 1,
  "id": 3,
  "type": "TIME"
}
```

The server returns the time in the response's `value` field.

The supported request and response types are represented as constants in the protocol package rather than using string literals throughout the implementation.

### 3.4 Error Handling

The protocol distinguishes between malformed messages and valid messages that violate protocol rules.

#### Malformed JSON

If the frame contains invalid JSON, the server cannot decode it into a request. The server closes the connection without sending an application-level error response.

For example:

```text
{"version":1,"id":1,"type":"PING"
```

This is not valid JSON, so it cannot be processed as a protocol request.

#### Protocol Validation Errors

A request can contain valid JSON but still violate the protocol rules.

For example:

```json
{
  "version": 1,
  "id": 2,
  "type": "ECHO"
}
```

The JSON is valid, but the message required by `ECHO` is missing.

In this case, the server can understand the request but cannot process it according to the protocol contract. It returns an `ERROR` response and then closes the connection.

#### Unknown Request Type

An unknown request type is also a protocol validation error.

For example:

```json
{
  "version": 1,
  "id": 3,
  "type": "HELLO"
}
```

The server returns an `ERROR` response containing the same request ID and then closes the connection.

#### Oversized Frames

The protocol limits the JSON payload to 1 MiB. A frame exceeding this limit is rejected before its payload is processed.

## 4. Implementing the Protocol

### 4.1 JSON Encoding and Decoding

The protocol package is responsible for converting Go request and response structures to and from JSON.

`protocol.Write` serializes a message using `json.Marshal`. The resulting JSON becomes the payload of a length-prefixed frame.

`protocol.Read` first reads the frame length, reads the complete payload, and then decodes the JSON using `json.Unmarshal`.

This keeps JSON handling separate from the TCP connection logic.

The overall process is:

```text
Write:

Go value
   ↓
json.Marshal
   ↓
JSON bytes
   ↓
Length-prefixed frame
   ↓
io.Writer


Read:

io.Reader
   ↓
Read frame length
   ↓
Read complete payload
   ↓
json.Unmarshal
   ↓
Go value
```

### 4.2 Framing

The protocol reuses the length-prefixed framing approach from the previous phase.

Before writing the JSON payload, the protocol encodes its size as a four-byte big-endian integer.

The writer also handles partial writes by continuing until all bytes have been written.

On the receiving side, the server first reads the length and then reads exactly that many payload bytes.

A maximum payload size of 1 MiB is enforced to prevent an unbounded allocation based on the length supplied by the peer.

### 4.3 Request Validation

After decoding a JSON message, the server validates the request before dispatching it.

Validation checks:

- protocol version is supported
- request ID is positive
- request type is supported
- `ECHO` contains a nonempty message

Validation is implemented separately from request dispatch. This allows the server to reject invalid requests before executing the operation.

A validation failure produces an `ERROR` response and marks the connection for closure.

### 4.4 Request Dispatch

Once a request passes validation, the server dispatches it based on its request type.

The dispatcher currently handles:

```text
PING → PONG
ECHO → ECHO
TIME → TIME
```

The dispatcher returns both the response and whether the connection should be closed.

This keeps request handling separate from connection management.

### 4.5 Connection Lifecycle

Each accepted TCP connection is handled by a separate goroutine.

The connection handler repeatedly:

1. Sets a read deadline.
2. Reads a framed request.
3. Decodes the JSON payload.
4. Dispatches the request.
5. Writes the response.
6. Closes the connection when a fatal condition occurs.
7. Otherwise, waits for the next request.

The connection therefore remains open for multiple valid request/response exchanges.

For a protocol error that can be represented as an `ERROR` response, the server writes the response first and then closes the connection.

For errors that prevent the server from interpreting the request, such as malformed JSON, the connection is closed without an application-level error response.

## 5. Experiments

I used a TCP client and server to verify the behavior of the protocol under both valid and invalid requests.

### 5.1 PING → PONG

I sent a `PING` request:

```json
{
  "version": 1,
  "id": 1,
  "type": "PING"
}
```

The server responded with:

```json
{
  "version": 1,
  "id": 1,
  "type": "PONG"
}
```

The request ID was preserved in the response.

### 5.2 ECHO

I sent an `ECHO` request containing a message:

```json
{
  "version": 1,
  "id": 2,
  "type": "ECHO",
  "message": "hello"
}
```

The server returned the same message:

```json
{
  "version": 1,
  "id": 2,
  "type": "ECHO",
  "message": "hello"
}
```

This verified that request-specific fields can be carried in the JSON message.

### 5.3 TIME

I sent a `TIME` request:

```json
{
  "version": 1,
  "id": 3,
  "type": "TIME"
}
```

The server returned the current UTC time in the `value` field.

### 5.4 Persistent Connections

I sent multiple valid requests over the same TCP connection:

```text
PING → PONG
ECHO → ECHO
TIME → TIME
```

The server processed each request and returned the corresponding response without requiring a new TCP connection.

This verified that the protocol supports persistent connections.

### 5.5 Unknown Request Type

I sent a request with an unsupported type:

```json
{
  "version": 1,
  "id": 4,
  "type": "HELLO"
}
```

The JSON was valid, but `HELLO` was not a supported request type.

The server returned:

```json
{
  "version": 1,
  "id": 4,
  "type": "ERROR",
  "message": "unknown request type "HELLO""
}
```

The server then closed the connection.

This demonstrated the distinction between a valid JSON message and a valid protocol request.

### 5.6 Malformed JSON

I deliberately sent a frame containing incomplete JSON:

```text
{"version":1,"id":1,"type":"PING"
```

The frame itself was valid, but the JSON payload was malformed.

The server failed while decoding the message:

```text
decode message: unexpected end of JSON input
```

No application-level error response was sent, and the connection was closed.

This demonstrated that malformed JSON is handled differently from a valid JSON request that violates protocol rules.

### 5.7 Missing ECHO Message

I sent a valid JSON request without the required `message` field:

```json
{
  "version": 1,
  "id": 2,
  "type": "ECHO"
}
```

The server successfully decoded the JSON but rejected the request during protocol validation.

It returned an `ERROR` response and then closed the connection.

This demonstrated the difference between JSON decoding and protocol validation.

### 5.8 Automated Tests

I added tests for:

- request serialization and deserialization
- oversized frame rejection
- request validation
- fatal protocol errors
- connection closure after a protocol error

I also ran the complete test suite with:

```sh
go test -v -count=1 ./...
```

All tests passed.

## 6. What I Learned

This phase helped me understand how an application protocol can be built on top of a transport layer.

I learned that framing and an application protocol solve different problems. Framing determines where a message begins and ends, while the application protocol defines what the message means and how the receiver should respond.

I also learned to separate different levels of errors. Invalid JSON cannot be interpreted as a request, so the connection is closed without an application-level response. A valid JSON message that violates the protocol can be understood, so the server can return an `ERROR` response before closing the connection.

Designing the protocol also helped me understand how request IDs provide identity and correlation without being an ordering mechanism. Persistent connections showed how multiple application-level request/response exchanges can share one TCP connection.

Finally, separating framing, JSON encoding/decoding, validation, dispatch, and connection management made the implementation easier to reason about and test.
