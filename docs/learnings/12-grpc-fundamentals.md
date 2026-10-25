# gRPC Fundamentals in Go

## Day 1 — Unary RPC, Deadlines, and Cancellation

### Goal

Build a minimal unary gRPC service in Go and understand the request-response lifecycle, deadlines, context cancellation, and automated testing.

### Implementation

- Defined an `InventoryService` with a unary `CheckStock` RPC using Protocol Buffers.
- Generated Go message types and gRPC client/server interfaces with `protoc`.
- Implemented a gRPC server and a Go client.
- Used `bufconn` to test the service without opening a real TCP listener.
- Added tests for stock availability, client deadlines, and server-side cancellation.

### Core Concepts

#### 1. Unary RPC

A unary RPC consists of one client request and one server response.

The client calls the generated `InventoryServiceClient`. The server implements the generated service interface and registers its implementation with a gRPC server.

#### 2. Deadlines

A client can set a deadline using `context.WithTimeout`.

When the deadline expires, the client can receive the gRPC status `DeadlineExceeded`. This limits how long the client waits for the operation.

A deadline does not forcibly terminate server-side code.

#### 3. Context Cancellation

The server handler receives a context associated with the RPC. When the client cancels the request or its deadline expires, the server can observe cancellation through `ctx.Done()` or `ctx.Err()`.

The handler and downstream operations must cooperate with cancellation. Work that ignores the context may continue after the client stops waiting.

#### 4. Context Propagation

Pass the request context through service and repository layers. Use context-aware APIs, such as `database/sql`'s `QueryContext`, where supported.

Context propagation allows cancellation and deadlines to reach downstream operations. Actual cancellation depends on the API and dependency supporting it.

#### 5. Concurrent Work

`errgroup.WithContext` can coordinate related concurrent tasks when failure in one task should cancel the others.

`g.Wait()` waits for all goroutines started through the group to return. It does not forcibly terminate goroutines. Each task must respond to cancellation.

### Testing

The fundamentals lab includes tests for:

- Available and unavailable products.
- A client deadline expiring during the slow RPC.
- The handler returning after explicit context cancellation.

Validation commands:

```bash
go test ./grpc/fundamentals/... -v
go test ./grpc/fundamentals/... -race
go vet ./grpc/fundamentals/...
```

All three commands passed during the initial validation run.

### Engineering Lessons

- A successful build does not prove that RPC behavior is correct; test the behavior.
- Deadlines and cancellation are related but distinct concepts.
- Context cancellation is cooperative, not forced termination.
- Downstream APIs must support context cancellation for it to stop underlying work.
- Tests should coordinate with the behavior they are testing rather than rely only on short sleeps or timing assumptions.
- Keep dependency and Go version changes deliberate and review them before committing.

### Scope and Limitations

This is a fundamentals lab, not a production inventory service. It uses deterministic in-memory behavior and an insecure transport configuration suitable for local development only.

Authentication, TLS, input validation, structured error handling, graceful shutdown, and production observability were outside Day 1's scope. Basic input validation and structured status errors are covered in [Day 2](#day-2--grpc-errors-and-request-validation).

## Day 2 — gRPC Errors and Request Validation

### Request Validation

`CheckStock` normalizes the product ID using `strings.TrimSpace(req.GetProductId())` before performing work. Empty strings and whitespace-only IDs become empty and return `codes.InvalidArgument`. Leading and trailing whitespace is also removed from otherwise valid IDs.

### API Contract

| Product ID | gRPC status | Response |
| --- | --- | --- |
| `""` or whitespace only | `InvalidArgument` | Error: `product_id must not be empty` |
| `"keyboard"` | `OK` | `available=true` |
| `"monitor"` | `OK` | `available=false` |
| `"unknown-product"` | `NotFound` | Error: `product not found` |

An existing product with no stock is a successful lookup. For `monitor`, the RPC returns a response with `available=false` and no error. `NotFound` means the product does not exist in the lab's hard-coded catalog; `InvalidArgument` means the request itself is invalid.

The `slow-item` path remains available for deadline and cancellation experiments. If its five-second operation completes without cancellation, it falls through to `NotFound` because it is not a catalog entry.

### Structured Errors and Context Handling

Clients and tests inspect `status.Code(err)` to handle errors programmatically. Status codes express the API contract; human-readable messages can change, so tests avoid matching error text.

The handler retains `status.FromContextError(err).Err()` to map `context.DeadlineExceeded` to `codes.DeadlineExceeded` and `context.Canceled` to `codes.Canceled`.

`codes.Internal` is intended for unexpected server failures, with implementation details kept out of client-facing messages. This lab does not yet simulate an internal failure or test that status.

### Tests and Validation

`TestCheckStockErrors` covers empty IDs, whitespace-only IDs, and unknown products through the in-memory gRPC server. The existing `monitor` test requires a successful RPC before checking `available=false`, explicitly distinguishing unavailable stock from a missing product. The deadline and explicit handler-cancellation tests continue to cover their separate behaviors.

The supplied Day 2 validation output shows all three checks passed:

```bash
go test ./grpc/fundamentals/... -v
go test ./grpc/fundamentals/... -race
go vet ./grpc/fundamentals/...
```

The catalog remains hard-coded for this lab. Authentication, TLS, graceful shutdown, and production observability remain outside the implemented scope. The optional, unexported `slowOperationStarted` channel supports test synchronization and is nil in production; broader instrumentation should use a cleaner separation as the service grows.

## Day 3 — gRPC Metadata and Correlation IDs

### Attaching and Reading Metadata

gRPC metadata carries key-value information alongside an RPC without adding fields to its Protocol Buffers request message. The client creates metadata with `metadata.Pairs` and attaches it to the outgoing request context:

```go
md := metadata.Pairs("x-correlation-id", "req-12345")
ctx = metadata.NewOutgoingContext(ctx, md)
```

On the server, `metadata.FromIncomingContext(ctx)` reads the incoming metadata. The `correlationIDFromContext` helper retrieves `x-correlation-id` and returns its first value. If incoming metadata is absent or the key has no values, it returns `"missing"`.

### Correlation IDs and Context Preservation

The client sends `x-correlation-id`, and the server includes it in the request log as `correlation_id`. This makes a request easier to identify in logs. Requests without the ID still work; `"missing"` is a logging fallback rather than a validation error.

The client attaches metadata to the existing context created by `context.WithTimeout`, preserving its one-second deadline and cancellation behavior. Creating a new context from `context.Background()` at this point would discard that timeout relationship.

The manual client-server run showed `correlation_id="req-12345"` in the server log for `slow-item`. The client received `DeadlineExceeded`, and the server observed `context deadline exceeded` instead of waiting for the five-second operation to complete. This verified metadata propagation alongside the existing cooperative cancellation behavior.

### Testing Strategy

`TestCorrelationIDFromContext` directly tests three parsing cases:

- Incoming metadata contains `req-test-123`: returns `req-test-123`.
- No incoming metadata: returns `"missing"`.
- Incoming metadata exists without `x-correlation-id`: returns `"missing"`.

The metadata cases use `metadata.NewIncomingContext`. These unit tests check parsing and fallback behavior; they do not prove that metadata crosses the gRPC client-server boundary. The manual run provided that end-to-end evidence.

Extracting the helper removed the test-specific `correlationIDObserved` field and its notification block from the production server. The existing `slowOperationStarted` channel remains for cancellation-test synchronization.

### Validation

The supplied post-refactor validation output shows the tests, race-detector check, and vet check passed:

```bash
go test ./grpc/fundamentals/... -v
go test ./grpc/fundamentals/... -race
go vet ./grpc/fundamentals/...
git diff --check
```

The race-detector result was cached. `git diff --check` reported no whitespace errors; its LF/CRLF warnings concerned line-ending conversion rather than test failures.

## Day 4 — Unary Server Interceptors

### Centralizing Request Logging

A unary server interceptor wraps an RPC handler with common behavior. Logging the RPC lifecycle in one place avoids repeating method, correlation ID, duration, and error logging in individual service methods.

`loggingUnaryInterceptor` uses the existing `correlationIDFromContext` helper. The duplicate request-start log and its unused correlation ID variable were removed from `CheckStock`; inventory validation and cancellation handling remain in the handler.

### Interceptor Flow and the Handler

The interceptor follows the `grpc.UnaryServerInterceptor` signature: it receives the request context, request, `grpc.UnaryServerInfo`, and a `grpc.UnaryHandler` callback.

For each unary RPC, the logging interceptor:

1. Extracts the correlation ID and records the start time.
2. Logs `rpc started` with `info.FullMethod` and the correlation ID.
3. Calls `handler(ctx, req)` to execute the next handler.
4. Logs `rpc completed` with the method, correlation ID, elapsed duration, and returned error.
5. Returns the original response and error unchanged.

If an interceptor never calls `handler`, the normal service business logic does not execute. It can instead return its own response or error. This logging interceptor always calls the handler and logs completion when it returns, including when it returns an error. It does not recover panics.

### Registration

Both `main()` and the `startTestServer` helper register the interceptor:

```go
server := grpc.NewServer(
    grpc.UnaryInterceptor(loggingUnaryInterceptor),
)
```

The in-memory test server is a separate server instance. Registering the interceptor only in `main()` would not exercise it through the `bufconn` tests.

### Observed Request Lifecycle

The supplied verbose test output shows one interceptor start entry and one completion entry for each RPC that returns. Completion logs include errors such as `InvalidArgument`, `NotFound`, and `DeadlineExceeded`, as well as `error=<nil>` for successful calls.

In the deadline run, the start log precedes the handler's cancellation log, followed by a completion log containing the handler's `DeadlineExceeded` error. The duration measures time around the handler call and logging; it is not an end-to-end client latency measurement.

`TestCheckStockSlowOperationCancellation` calls the service handler directly, so it bypasses the interceptor. The RPC-based tests exercise the registered interceptor; their log output provides evidence that it runs, without automated assertions on log formatting.

### Focused Tests

`TestLoggingUnaryInterceptor` calls the interceptor directly without a running gRPC server:

- `passes through response` verifies that the handler is invoked, no error is introduced, and the expected response is returned.
- `passes through error` verifies that the exact original handler error is returned, without wrapping or replacement.

These unit tests check the interceptor's contract in isolation. The existing in-memory RPC tests check service behavior with the interceptor registered. Neither replaces the other, and the focused tests do not assert log contents or exact durations.

### Validation

The supplied Day 4 validation output shows all three checks passed:

```bash
go test ./grpc/fundamentals/... -v
go test ./grpc/fundamentals/... -race
go vet ./grpc/fundamentals/...
```

## Day 5 — Chaining Unary Server Interceptors

### Purpose and Demonstration Interceptor

Multiple interceptors let a service compose common responsibilities, such as logging, authentication, metrics, or recovery, around its business handler. Their order determines which behavior wraps the rest of the request lifecycle.

For this experiment, `tracingUnaryInterceptor` logs `TRACE: entering` with the full RPC method, calls `handler(ctx, req)`, logs `TRACE: exiting` when that call returns, and returns the original response and error.

This interceptor demonstrates chaining. It is not a production tracing implementation: it creates no distributed tracing spans and overlaps with the existing logging interceptor's start and completion logs. It remains in the lab for this milestone; a distinct responsibility would be more useful as the service grows.

### Registration and Execution Order

Both the production server and the in-memory test server now register the same chain:

```go
server := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        loggingUnaryInterceptor,
        tracingUnaryInterceptor,
    ),
)
```

`grpc.ChainUnaryInterceptor` composes the interceptors in registration order, with the first interceptor outermost. Each interceptor's `handler` callback invokes the remaining chain, eventually reaching the service method.

When each interceptor calls its next handler and those calls return normally, execution follows this order:

```text
logging: before
tracing: before
service handler
tracing: after
logging: after
```

The observed RPC logs show `rpc started`, `TRACE: entering`, `TRACE: exiting`, and `rpc completed` in that order, with service work occurring between the trace entries.

### Errors and Short-Circuiting

A returned error does not prevent the code after `handler(ctx, req)` from running. The tracing interceptor logs its exit, then returns the error unchanged; the outer logging interceptor records the returned error and completes. The supplied RPC output demonstrates this for `InvalidArgument`, `NotFound`, and deadline cancellation.

An interceptor can short-circuit the remaining chain by returning without invoking its next handler. Later interceptors and the service handler are then skipped. An outer interceptor that already called this inner interceptor can still resume after it returns. This describes ordinary returns, including errors; neither lab interceptor recovers panics.

The manual deadline experiment also showed different client and server perspectives: the client reported `DeadlineExceeded`, while the server observed `context.Canceled` and returned `Canceled`. The client can reach its own deadline while the server observes cancellation of the RPC. Server processing duration is not an exact end-to-end client latency measurement.

### Testing the Contract and the Chain

- `TestTracingUnaryInterceptor` verifies that the next handler is called and its successful response is returned without an added error.
- `TestTracingUnaryInterceptorPassesThroughError` verifies that the exact original handler error is returned without replacement or wrapping.
- `TestUnaryInterceptorExecutionOrder` uses a sequence slice to assert manually nested interceptor execution. It demonstrates nesting without relying on log formatting, but does not invoke `grpc.ChainUnaryInterceptor` or the production interceptors.
- The existing RPC tests use `bufconn` with the actual logging and tracing chain registered. Their supplied verbose output shows both interceptors running while stock, validation, and deadline behavior remains intact. Execution order in this RPC path was inspected through logs rather than asserted by a sequence test.

`TestCheckStockSlowOperationCancellation` continues to call the service method directly and bypasses both interceptors.

### Validation

The supplied Day 5 output shows the full test suite, race-detector check, and vet check passed after registering the chain in the test server:

```bash
go test ./grpc/fundamentals/... -v
go test ./grpc/fundamentals/... -race
go vet ./grpc/fundamentals/...
```

## Day 6 — Unary Authentication Interceptor

### Reading and Validating Credentials

`authenticationUnaryInterceptor` reads incoming metadata with `metadata.FromIncomingContext(ctx)` and retrieves the first `authorization` value. It requires the exact `Bearer ` prefix and a nonempty token matching `labAuthToken`.

Missing incoming metadata, an absent authorization value, a malformed prefix, and an empty or incorrect token return `codes.Unauthenticated`. Only a valid token proceeds to `handler(ctx, req)`. Clients and tests inspect the status code rather than matching error messages.

### Short-Circuiting the Chain

The production server and the in-memory test server register the same chain:

```go
grpc.ChainUnaryInterceptor(
    loggingUnaryInterceptor,
    tracingUnaryInterceptor,
    authenticationUnaryInterceptor,
)
```

Logging and tracing wrap authentication. If authentication returns an error without calling its next handler, the inventory service is skipped. The outer interceptors still resume, allowing tracing to log its exit and logging to record the rejected RPC's completion and error.

### Authenticated Client and Test Contexts

The client attaches `authorization: Bearer lab-secret-token` using `metadata.NewOutgoingContext` on its existing one-second timeout context. Authenticated RPC tests also attach the valid token while retaining their deadlines. Authentication remains enabled in the test server; tests supply credentials rather than bypassing the interceptor.

The current example client sends authorization metadata without a correlation ID, so request logs use the existing `"missing"` correlation ID fallback.

### Unit and RPC Tests

The focused authentication tests use incoming contexts directly:

- A valid token calls the next handler, returns its known response, and produces no error.
- Missing metadata returns `Unauthenticated` without calling the handler.
- An incorrect token returns `Unauthenticated` without calling the handler.

The interceptor also rejects malformed and empty credentials, but those branches do not yet have dedicated unit tests.

`TestCheckStockUnauthenticated` exercises the actual server chain through the generated client and `bufconn`. It sends `slow-item` without authorization metadata, requires an error with status `Unauthenticated`, and checks the buffered `slowOperationStarted` channel after the RPC returns. The empty buffer confirms that the slow-operation branch was not reached, without a sleep or reliance on log output.

That notification is specific to the slow branch, rather than an independent observation of every handler entry. The authentication interceptor's early return and its unit tests establish that the next handler is not invoked for rejected credentials. Existing authenticated stock, validation, and deadline RPC tests continue to pass. The direct handler-cancellation test bypasses authentication and the other interceptors.

### Validation

The final Day 6 validation passed with these commands:

```bash
go test ./grpc/fundamentals/... -v
go test ./grpc/fundamentals/... -race
go vet ./grpc/fundamentals/...
git diff --check
```

The final full-suite and race-detector runs used cached successful test results. Vet reported no issues, and the whitespace check passed.

### Lab Limitations

The shared token is hard-coded for learning purposes, and the local client still uses insecure transport. This demonstrates authentication middleware and rejection behavior, not production credential management or transport security. It does not implement token expiry, rotation, identity claims, or authorization policies. Authentication establishes whether credentials are accepted; authorization determines what an authenticated caller may do.

## Day 7 — Server Streaming Fundamentals

### Objective and Protocol Buffers

Implement and test a server-streaming RPC: one client request can receive multiple server responses.

The service definition adds `ListStock`, which returns `stream StockItem`:

```proto
rpc ListStock(StockListRequest) returns (stream StockItem);
```

The Go Protobuf message types and gRPC client/server code were regenerated after updating the schema.

### Sending Items and Handling Cancellation

The server calls `stream.Send(item)` for each stock item individually, in order: `keyboard` with `available=true`, `monitor` with `available=false`, and `mouse` with `available=true`. A send error is returned immediately; normal completion returns nil.

Before each send, the handler checks `stream.Context().Err()`. During the optional test coordination wait, it also selects on `stream.Context().Done()`. Context errors are mapped with `status.FromContextError`, preserving `Canceled` or `DeadlineExceeded`. Cancellation remains cooperative: the server must observe the stream context and stop its work.

### Testing the Stream

`TestListStock` calls the generated client's `ListStock` method with an empty `StockListRequest`, then repeatedly calls `Recv()`. `io.EOF` means normal stream completion; any other receive error fails the test. The test asserts exactly three items with the expected IDs, availability values, and order.

`TestListStockCancellation` receives the first item and waits for the server's buffered `firstStockItemSent` notification. The server then waits on `continueAfterFirstStockItem` or context cancellation. The test cancels its context without releasing the continuation channel and checks that the next `Recv()` returns `codes.Canceled`.

These channels coordinate cancellation after the first send without arbitrary sleeps. The notification wait has a timeout as a failure guard. The client-side cancellation assertion does not independently prove the server handler has returned; the server's context-aware wait provides the cancellation path.

The streaming tests attach valid authorization metadata, but the existing interceptors are unary interceptors and do not run for `ListStock`. Stream authentication and other stream interceptors are outside this day's implementation.

### Validation and Status

The reported Day 7 validation results were successful:

```bash
go test ./grpc/fundamentals/...
go test -race ./grpc/fundamentals/...
go vet ./grpc/fundamentals/...
```

Status: Completed for the Day 7 server-streaming fundamentals lab.

## Day 8 — Server-Streaming Client and Lifecycle

### Consuming a Complete Stream

`listStock` calls the generated client's `ListStock` method and repeatedly calls `Recv()` to consume individual stock messages. It logs each product and its availability. `io.EOF` means the stream completed normally, so the helper logs completion and returns nil. Other errors are wrapped with context using `%w`, distinguishing stream creation from message-receive failures.

The manual client-server run received `keyboard` (`true`), `monitor` (`false`), and `mouse` (`true`), followed by `Stock stream completed`.

### Limiting Consumption and Cancelling the Child Context

`listFirstStockItems` accepts a configurable positive limit and rejects nonpositive values before starting an RPC. It creates a child context with `context.WithCancel(ctx)` and defers cancellation. The receive loop stops after at most the requested number of items; if the server finishes earlier, `io.EOF` is still successful completion.

Returning after reaching the limit runs the deferred `cancel()`, requesting cancellation of this stream. Cancelling the child does not cancel its parent context. Cancellation remains cooperative on the server.

The client preserves the original unary example with its one-second deadline and creates a separate five-second context for the streaming examples. Both streaming calls use that parent context and its outgoing authorization metadata; their five-second budget is shared across the sequential calls. The limited helper's child inherits that deadline and metadata.

### Manual Observations and Their Limits

The manual run showed normal completion for the full stream, then only `keyboard` and `monitor` for the limited call with a limit of two. The limited call did not print `mouse`.

This confirms that the client stops consuming after two items. The three-item server may already have sent every item before cancellation arrives, so the manual output does not prove that server-side processing stopped immediately. Cancellation is requested by the deferred call when the helper returns, rather than by its preceding log statement.

The unary `slow-item` call still reports `DeadlineExceeded` as expected. The server can observe `context.Canceled` while the client reports its own expired deadline; these are different perspectives on the RPC lifecycle.

### Reusing Controlled Cancellation Coverage

Day 7's `TestListStockCancellation` supplies a controlled scenario: channels pause the server after the first send, the client cancels, and the next receive reports `Canceled`. The server's wait selects on its context's cancellation signal. This exercises the cancellation path without arbitrary sleeps or a large generated inventory, so Day 8 does not add a duplicate test.

As recorded in Day 7, the client-side receive assertion does not independently confirm the exact time the server handler returns. The client helpers themselves currently have no dedicated unit tests; their normal and limited consumption behavior was checked through the manual run.

### Validation

The supplied Day 8 output shows formatting, tests, race detection, vet, and the whitespace check completed successfully:

```bash
gofmt -w grpc/fundamentals/client/main.go
go test ./grpc/fundamentals/...
go test -race ./grpc/fundamentals/...
go vet ./grpc/fundamentals/...
git diff --check
```

The server test results were cached in both test runs. The LF/CRLF message from Git was a line-ending warning; no whitespace errors were reported.

## Day 9 — Client-Streaming RPC

### Objective

Implement a client-streaming RPC in which the client sends multiple messages and receives one final response.

### What I Implemented

- Added the `ReportStock` RPC to `inventory.proto`.
- Defined `StockReportResponse` with `received_count` and `available_count`.
- Regenerated the Go Protobuf and gRPC code.
- Implemented the server handler to receive stock items until `Recv()` returns `io.EOF`.
- Used `SendAndClose()` to return the final report.
- Implemented the client to send multiple stock items using `Send()`.
- Used `CloseAndRecv()` to finish sending and receive the final response.
- Added automated tests for multiple items, an empty stream, and a single available item.

### Client-Streaming Lifecycle

1. The client opens the stream.
2. The client sends zero or more messages.
3. The client calls `CloseAndRecv()`.
4. The server receives messages until `io.EOF`.
5. The server sends one final response using `SendAndClose()`.
6. The client receives the response or an error.

### Validation

The reported Day 9 validation results were successful:

- `gofmt` completed successfully.
- `go test ./grpc/fundamentals/...` passed.
- `go test -race ./grpc/fundamentals/...` passed.
- `go vet ./grpc/fundamentals/...` passed.

### Key Learning

Client streaming is useful when a client needs to submit multiple messages and receive a single aggregated result, such as a batch report or a collection of telemetry records.

Lab note: The current authentication, logging, and tracing interceptors are unary interceptors. They do not automatically apply to streaming RPCs.

## Day 10 — Bidirectional Streaming RPC

### Objective

Implement an RPC where the client and server can both send multiple messages over a single stream.

### What I Implemented

- Added the `SyncStock` RPC to `inventory.proto`.
- Regenerated the Go Protobuf and gRPC code.
- Implemented the server handler to receive stock items and send each item back to the client.
- Implemented the client to send multiple stock items, call `CloseSend()`, and receive responses until `io.EOF`.
- Added an end-to-end example demonstrating three stock items sent and received.
- Added automated tests for normal completion and client-observed cancellation.

### Bidirectional Streaming Lifecycle

1. The client opens the stream.
2. The client sends multiple messages using `Send()`.
3. The server receives messages using `Recv()` and sends responses using `Send()`.
4. The client calls `CloseSend()` when it has finished sending.
5. The client continues receiving responses until `io.EOF` or an error occurs.

### Validation

The reported Day 10 validation results were successful:

- `go test ./grpc/fundamentals/...` passed.
- `go test -race ./grpc/fundamentals/...` passed.
- `go vet ./grpc/fundamentals/...` passed.
- `git diff --check` reported no whitespace errors.

### Key Learning

Bidirectional streaming supports independent sending and receiving directions within one RPC. Closing the client's sending direction does not close the receiving direction.

Testing note: The cancellation test verifies that the client observes cancellation. It does not independently prove that the server handler has stopped processing.

Lab note: The existing authentication, logging, and tracing interceptors are unary interceptors and do not automatically apply to streaming RPCs.
