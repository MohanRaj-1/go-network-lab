# gRPC Fundamentals in Go

## Goal

Build a minimal unary gRPC service in Go and understand the request-response lifecycle, deadlines, context cancellation, and automated testing.

## Implementation

- Defined an `InventoryService` with a unary `CheckStock` RPC using Protocol Buffers.
- Generated Go message types and gRPC client/server interfaces with `protoc`.
- Implemented a gRPC server and a Go client.
- Used `bufconn` to test the service without opening a real TCP listener.
- Added tests for stock availability, client deadlines, and server-side cancellation.

## Core Concepts

### 1. Unary RPC

A unary RPC consists of one client request and one server response.

The client calls the generated `InventoryServiceClient`. The server implements the generated service interface and registers its implementation with a gRPC server.

### 2. Deadlines

A client can set a deadline using `context.WithTimeout`.

When the deadline expires, the client can receive the gRPC status `DeadlineExceeded`. This limits how long the client waits for the operation.

A deadline does not forcibly terminate server-side code.

### 3. Context Cancellation

The server handler receives a context associated with the RPC. When the client cancels the request or its deadline expires, the server can observe cancellation through `ctx.Done()` or `ctx.Err()`.

The handler and downstream operations must cooperate with cancellation. Work that ignores the context may continue after the client stops waiting.

### 4. Context Propagation

Pass the request context through service and repository layers. Use context-aware APIs, such as `database/sql`'s `QueryContext`, where supported.

Context propagation allows cancellation and deadlines to reach downstream operations. Actual cancellation depends on the API and dependency supporting it.

### 5. Concurrent Work

`errgroup.WithContext` can coordinate related concurrent tasks when failure in one task should cancel the others.

`g.Wait()` waits for all goroutines started through the group to return. It does not forcibly terminate goroutines. Each task must respond to cancellation.

## Testing

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

## Engineering Lessons

- A successful build does not prove that RPC behavior is correct; test the behavior.
- Deadlines and cancellation are related but distinct concepts.
- Context cancellation is cooperative, not forced termination.
- Downstream APIs must support context cancellation for it to stop underlying work.
- Tests should coordinate with the behavior they are testing rather than rely only on short sleeps or timing assumptions.
- Keep dependency and Go version changes deliberate and review them before committing.

## Scope and Limitations

This is a fundamentals lab, not a production inventory service. It uses deterministic in-memory behavior and an insecure transport configuration suitable for local development only.

Authentication, TLS, input validation, structured error handling, graceful shutdown, and production observability are outside this day's scope.
