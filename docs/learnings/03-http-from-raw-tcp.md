# HTTP from Raw TCP

## Goal

In the previous experiments, I learned that TCP provides an ordered byte stream and that an application protocol must define its own message boundaries. I also built a small application protocol using length-prefixed JSON messages.

In this phase, I applied those ideas to HTTP. The goal was to understand how HTTP requests and responses are reconstructed from raw TCP bytes, without using `net/http` or a router package.

I started with a server that accepted one connection, printed the raw headers, and closed. I gradually added parsing, body framing, routing, persistent connections, concurrency, and validation.

The server listens on `:8083`. It implements a deliberately small HTTP/1.1 subset. The rules described below reflect this experiment, rather than a complete HTTP implementation.

## 1. HTTP Request Structure

A request contains a request line, header fields, a blank line, and possibly a body.

```text
POST /echo HTTP/1.1
Host: localhost
Content-Length: 5

hello
```

The actual line endings are carriage return followed by line feed, written as `\r\n`:

```text
POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 5\r\n\r\nhello
```

The sequence `\r\n\r\n` marks the end of the header section. It does not necessarily mark the end of the request. In this example, five body bytes still belong to the request.

I used `bytes.Index` to find the separator in the accumulated buffer:

```go
end := bytes.Index(r.buffer, []byte("\r\n\r\n"))
```

A result of `-1` means the separator has not arrived. Otherwise, `end` is its starting byte index. The header section ends at `end + 4`, including the separator.

## 2. Request Line

The first line describes the operation, target, and version:

```text
GET / HTTP/1.1
```

I extract three components:

```text
method  = GET
target  = /
version = HTTP/1.1
```

Initially, I used `strings.Fields` to split the line. Later, I changed it to `strings.Split(line, " ")` so repeated spaces produce empty components instead of being silently collapsed.

The current parser requires exactly three components. It checks the method's token syntax, requires a target beginning with `/`, rejects tabs and line breaks in the target, and accepts only `HTTP/1.1`.

A syntactically valid method such as `PUT` is different from a malformed method. Routing decides whether that valid method is supported for the target.

The target is currently used as an exact string. I have not added URL parsing or separate path/query handling.

## 3. HTTP Headers

Each header field has a name followed by a colon and a value:

```text
User-Agent: curl/8.21.0
```

I split at the first colon:

```go
name, value, found := strings.Cut(line, ":")
```

For this example:

```text
name  = "User-Agent"
value = " curl/8.21.0"
found = true
```

I do not use `strings.Fields` for headers because a value can contain spaces:

```text
X-Test: hello world
```

It can also contain additional colons:

```text
X-Test: hello:world
```

Splitting only at the first colon preserves the complete value. The generic parser stores the value; header-specific code decides what it means.

## 4. Header Names and Values

Names and values have different validation rules.

**Field names** must be nonempty and contain only ASCII letters, digits, or these token punctuation characters:

```text
! # $ % & ' * + - . ^ _ ` | ~
```

Examples:

| Name              | Result   |
| ----------------- | -------- |
| `Content-Type`    | Accepted |
| `X-Test2`         | Accepted |
| `X_Request`       | Accepted |
| `X.Test`          | Accepted |
| `Host Name`       | Rejected |
| `Host@`           | Rejected |
| Empty name        | Rejected |
| Non-ASCII letters | Rejected |

I implemented `isValidHeaderName` using byte indexing. `unicode.IsLetter` would accept letters outside ASCII, which is not the field-name rule I want to enforce.

The raw name is validated before lowercasing. I no longer trim the name first, because that would hide invalid whitespace in a field such as `Host : example.com`.

**Field values** are validated as bytes too, but their allowed set is broader. The helper rejects bytes `0x00` through `0x1F`, except horizontal tab (`0x09`), and also rejects DEL (`0x7F`). Other bytes are accepted by the generic validator, including bytes above `0x7F`; it does not require valid UTF-8.

| Value                   | Generic result |
| ----------------------- | -------------- |
| `hello world`           | Accepted       |
| `hello:world`           | Accepted       |
| `!@#$%^&*()`            | Accepted       |
| Empty value             | Accepted       |
| Internal horizontal tab | Accepted       |
| Embedded CR or LF       | Rejected       |
| NUL or DEL              | Rejected       |

After raw validation, I trim only surrounding ASCII spaces and tabs:

```go
strings.Trim(value, " \t")
```

`strings.TrimSpace` removes a broader set, including some forbidden controls and Unicode whitespace. Using it before validation could turn malformed input into apparently valid input. Internal spaces and tabs are preserved, and arbitrary values are never lowercased.

A subtle example is `Host:: example.com`. Splitting at the first colon gives the valid name `Host` and the value `: example.com`. Name validation alone cannot reject it. Our current Host semantics only check count and nonemptiness, not complete Host-value syntax.

## 5. Repeated Headers

I changed the representation from `map[string]string` to `map[string][]string`:

```go
type Request struct {
    Method  string
    Target  string
    Version string
    Headers map[string][]string
    Body    []byte
}
```

For example:

```text
Accept: text/html
ACCEPT: application/json
X-Test: one
X-Test: two
```

becomes:

```text
accept -> ["text/html", "application/json"]
x-test -> ["one", "two"]
```

After validating the name and raw value, the parser appends each occurrence:

```go
name = strings.ToLower(name)
request.Headers[name] = append(request.Headers[name], strings.Trim(value, " \t"))
```

This preserves arrival order within each name. The map does not preserve the overall order between different names, so printed header order can vary.

Appending preserves the evidence; it does not mean every duplicate is valid.

The caller must interpret all relevant values. For example, repeated Content-Length values must agree, while repeated Connection fields are scanned for tokens. I did not introduce a generic helper that silently chooses the first value.

A comma is not a universal instruction to split every header. It has meaning according to the particular field. Connection processing recognizes comma-separated tokens; arbitrary X-Test values remain intact.

## 6. Host Header Semantics

For this HTTP/1.1 learning server, I require exactly one nonempty Host value.

| Input                     | Result   |
| ------------------------- | -------- |
| `Host: example.com`       | Accepted |
| Missing Host              | Rejected |
| `Host:`                   | Rejected |
| Two identical Host fields | Rejected |
| Two different Host fields | Rejected |
| `host: example.com`       | Accepted |

The parser normalizes the name to `host`, so the semantic validator does not need separate capitalization cases.

`validateRequestSemantics` lives in `request.go`. It runs after header parsing but before selecting or reading the body framing.

```text
Parse headers -> Validate Host -> Determine framing -> Read body
```

For an invalid Host on a POST, the body must not be consumed by the body reader. A header read can already have received body bytes; those bytes remain unconsumed in `r.buffer` when semantic validation fails. If they have not arrived in the buffer, no extra body read is attempted.

## 7. Message Body Framing

Finding the header terminator tells me where the body begins. Framing tells me where that body ends.

### Content-Length

```text
POST /echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 5\r\n\r\nhello
```

Content-Length counts body bytes only. It does not count the request line, headers, or separator.

The current code uses `strconv.Atoi`, rejects invalid or negative values, and checks every repeated value. Two values of `5` are accepted and preserved. Values of `5` and `10` conflict and are rejected before body reading. Comparison is numeric, so `05` and `5` currently agree; this is not a full strict decimal-syntax validator.

`readBytes` allocates a slice of the required size and first copies bytes already buffered:

```go
copied := copy(data, r.buffer)
r.buffer = r.buffer[copied:]
```

`copy` never copies more than fits in the destination. If the body is five bytes and the buffer contains `helloGET /...`, only `hello` is consumed. The GET remains buffered.

When bytes are missing, the helper uses:

```go
io.ReadFull(r.reader, data[copied:])
```

If `he` was buffered, the remaining slice has room for three bytes. ReadFull reads `llo` and does not consume the next request.

I do not use `io.ReadAll(conn)` for a request body. On a persistent connection, that would wait for the stream to end rather than stopping at this request's body boundary.

Without either supported framing header, our server treats the request as having an empty body.

### Transfer-Encoding: chunked

Chunked encoding describes each piece separately, so the client does not need to declare the total body length first.

```text
5\r\nhello\r\n8\r\n network\r\n0\r\n\r\n
```

This decodes to `hello network`, which is 13 bytes. The space before `network` belongs to the second chunk.

A nonzero chunk has:

```text
<hexadecimal size>\r\n<exactly that many data bytes>\r\n
```

Sizes are hexadecimal: `A` means 10 bytes and `10` means 16 bytes.

`readChunkedBody` repeatedly reads a size line, validates hexadecimal digits, converts the size, reads exactly that many bytes, and validates the following CRLF. It appends only chunk data to the decoded body.

For the zero chunk, it consumes the final blank line and returns. Our subset requires the ending `0\r\n\r\n`; chunk extensions and trailers are not supported.

The helpers preserve unconsumed buffer bytes, including a request immediately following the final chunk. Incomplete size lines, truncated data, invalid hexadecimal sizes, and incorrect CRLF endings produce errors.

Repeated Transfer-Encoding fields are joined as an ordered coding list. Our server accepts only a single `chunked` coding, ignoring its case. It rejects unsupported lists, repeated chunked coding, and any request combining Transfer-Encoding with Content-Length.

## 8. Persistent Connections

A connection handler now loops instead of returning after its first response:

```text
read request -> validate -> route -> write response -> read next request
```

For accepted HTTP/1.1 requests, the connection stays open by default. Each connection retains the same `requestReader` across iterations.

Connection fields can contain multiple tokens and can occur multiple times. The server scans every value, splits that field's comma-separated tokens, and compares trimmed tokens without case sensitivity.

Any `close` token causes the handler to close after sending the response. It wins even if another value says `keep-alive`. Other tokens are ignored by this small implementation.

A normal EOF between requests ends the handler quietly. EOF during an incomplete request is treated as unexpected EOF. The handler attempts a 400 response for a read/parse error other than normal EOF, then closes; a broken connection may prevent that response from being delivered.

## 9. HTTP Pipelining

Persistence allows connection reuse. Pipelining means the client sends another request before waiting for the earlier response.

One TCP read can contain:

```text
POST headers + hello + GET headers
```

After reading the POST's five-byte body, the server must preserve the GET. Discarding the buffer would lose a valid request.

The next call to `readRequest` searches the existing buffer before reading from TCP. A complete buffered request can therefore be parsed immediately.

Our handler processes requests sequentially within one connection and sends responses in that same order. It does not launch a goroutine for every pipelined request.

## 10. Fragmented TCP Reads

A single request can arrive in many pieces. Even a delimiter can be split:

```text
Read 1: ...\r
Read 2: \n\r
Read 3: \nhello
```

The parser appends incoming bytes and searches the accumulated data. Body readers consume buffered bytes before requesting more from the underlying reader.

`requestReader` also remembers an error returned with a read. Go readers can return both bytes and an error, so those bytes must be considered before deciding that the request is incomplete.

For deterministic fragmentation tests, I used `bytewiseReader`, which allows at most one byte per Read. This is stronger than assuming that two client Write calls will produce two server Read calls. TCP does not promise that mapping.

## 11. Request Validation

I separated the questions of syntax, semantics, and framing:

```text
Raw bytes
    |
Parse request line and headers; enforce parsing limits
    |
Validate request semantics, starting with Host
    |
Choose and read body framing
    |
Route and respond
```

The files currently contain:

| File           | Current responsibility                                                    |
| -------------- | ------------------------------------------------------------------------- |
| `main.go`      | Listen, accept, manage handlers, and route                                |
| `request.go`   | Request model, Host semantics, printing, and connection policy            |
| `parser.go`    | Header reading, limits, syntax, normalization, and semantic validation call |
| `framing.go`   | Framing-header validation, body selection, stateful reader, and body-reading helpers |
| `response.go`  | Encode and write responses                                                |
| `main_test.go` | Parser, framing, validation, and connection tests                         |

I moved Content-Length and Transfer-Encoding validation and body-framing selection into `readBody` in `framing.go`. After parsing headers and validating request semantics, `readRequest` in `parser.go` calls `readBody` and stores the returned bytes in `request.Body`. This keeps framing decisions together with the body-reading helpers.

The parser enforces these experiment-specific limits:

```go
const (
    MaxHeaderBytes = 8 * 1024
    MaxHeaderLine  = 4 * 1024
    MaxHeaderCount = 100
)
```

The total includes the request line, headers, and final CRLF CRLF. The line limit applies to parsed line content, excluding CRLF, including the request line. Header count counts field occurrences, including duplicates, but excludes the request line.

Before reading more header bytes, the parser checks for the separator and calculates remaining capacity. With 8190 bytes buffered, it requests at most two more bytes. A terminator ending at byte 8192 is accepted. At 8192 bytes without a terminator, it rejects without another Read.

If data is already buffered, only bytes through the header terminator count toward the current header limit. A body or another request may make the whole buffer larger without making the current headers oversized.

Line length and count are currently checked after the complete header section is available. The total limit bounds the preceding header reads.

These protections do not make the server complete. It has no HTTP connection deadlines, configured maximum body size, or chunk-size-line limit yet. Header limits bound header accumulation but do not prevent a client from stalling below the limit. Host-value syntax and full target syntax also remain deliberately limited.

## 12. HTTP Responses

The server writes response bytes directly instead of using `net/http`.

```text
HTTP/1.1 200 OK\r\nContent-Length: 12\r\nContent-Type: text/plain\r\nConnection: keep-alive\r\n\r\nHello World!
```

`Hello World!` is 12 bytes with no trailing newline. The writer calculates Content-Length using `len(body)` and handles partial writes by continuing until all response bytes are sent or an error occurs.

Our routing and error rules are:

| Request or condition                   | Response                       |
| -------------------------------------- | ------------------------------ |
| `GET /`                                | 200, `Hello World!`            |
| `POST /echo`                           | 200, decoded request body      |
| Unknown target                         | 404, `Not Found`               |
| Wrong method for `/` or `/echo`        | 405, `Method Not Allowed`      |
| Unsupported version or invalid request | 400, `Bad Request`, then close |

The target is checked first when routing, so `PUT /unknown` returns 404, while `PUT /` returns 405. These are our chosen subset rules. The current 405 response does not include an Allow header.

Even if the request is chunked, the response uses Content-Length because the complete decoded body is already available.

## 13. Concurrency

`run` repeatedly accepts connections and starts a handler goroutine:

```go
go handleConnection(conn)
```

If client A sends only `GET /` and stops, A's handler can block while the listener accepts client B and B's handler serves a complete request.

Each handler owns its request reader and buffer. Requests on different connections can progress independently. Requests within one connection remain sequential.

`defer conn.Close()` releases the connection when its handler returns. Concurrent request logging can interleave because handlers print independently.

## 14. What I Learned

The biggest connection to the earlier phases was seeing that HTTP still needs application-level framing. TCP does not know where an HTTP header section, body, or request ends.

I initially treated the first header separator as the end of a request. Adding POST showed why that was incomplete. Content-Length and chunked encoding give the body its boundary, and preserving leftover bytes makes persistence and pipelining possible.

I also learned to separate parsing from interpretation. Parsing records what was sent; semantics decides whether those fields make sense. Preserving repeated values makes that decision possible instead of silently overwriting evidence.

Header names and values taught me why validation must match the protocol's byte rules. Names have a narrow ASCII token set. Values allow more bytes, and cleanup must not erase invalid input before validation.

The boundary tests changed how I thought about limits. It is not enough to reject an oversized buffer eventually. The next Read must respect remaining capacity, and the parser must still accept a terminator exactly at the boundary.

Finally, separating connection management, parsing, semantic validation, framing helpers, and response writing made the code easier to discuss and test. I can now follow a request from raw bytes through a structured Request to an HTTP response.

## 15. Experiments and Tests

The commands below run from the project root. I use `curl.exe` on Windows to invoke curl explicitly, and double quotes so the examples work in Command Prompt as well as PowerShell.

Start the server in one terminal:

```sh
go run ./tcp/http/server
```

In another terminal, test the root route:

```sh
curl.exe -i http://127.0.0.1:8083/
```

Expected: 200 with `Hello World!`.

Test an unknown route and an unsupported method on a known route:

```sh
curl.exe -i http://127.0.0.1:8083/unknown
curl.exe -i -X PUT http://127.0.0.1:8083/
```

Expected: 404 and 405 respectively.

Test a Content-Length body:

```sh
curl.exe -i -X POST http://127.0.0.1:8083/echo -d "hello network"
```

Expected: 200 with `hello network`, a 13-byte body.

Test a chunked upload:

```sh
curl.exe -i --http1.1 -H "Transfer-Encoding: chunked" -X POST http://127.0.0.1:8083/echo -d "hello network!"
```

Curl generates the chunk framing. The server returns the decoded text with Content-Length 14. The exclamation mark adds the extra byte.

The automated tests cover:

| Test                                  | What it verifies                                                                            |
| ------------------------------------- | ------------------------------------------------------------------------------------------- |
| `TestReadRequestPreservesPipeline`    | A POST body does not consume the next request                                               |
| `TestTruncatedRequest`                | Incomplete headers/bodies report unexpected EOF                                             |
| `TestConcurrentPersistentConnections` | A stalled client does not block another; pipelined responses stay ordered; close is honored |
| `TestHTTPStatusResponses`             | Response status, body, length, and closure for selected cases                               |
| `TestChunkedPipeline`                 | Chunked decoding and preservation of a following GET, including bytewise reads              |
| `TestChunkedBody`                     | Empty/hexadecimal chunks and malformed framing                                              |
| `TestRepeatedHeaders`                 | Duplicate preservation, value casing, and Connection tokens                                 |
| `TestRepeatedFramingHeaderCallers`    | Conflicting lengths and unsupported coding lists                                            |
| `TestHostSemantics`                   | Missing, empty, duplicated, and lowercase Host cases                                        |
| `TestInvalidHostDoesNotConsumeBody`   | Invalid Host leaves buffered or unread body bytes untouched                                 |
| `TestHeaderLimits`                    | Exact boundaries, excess bytes/lines/count, duplicates, and extra buffered data             |
| `TestIsValidHeaderName`               | ASCII token-name rules                                                                      |
| `TestHeaderNameParsing`               | Raw name validation before normalization                                                    |
| `TestIsValidHeaderValue`              | All 256 byte values and the HTAB exception                                                  |
| `TestHeaderValueParsing`              | Raw controls rejected; only surrounding space/tab trimmed                                   |

`headerBudgetReader` fails a test if the parser requests more bytes than its remaining allowance or reads again after that allowance is exhausted. It verifies the actual read behavior, not just the final error.

The Host/body tests distinguish bytes already fetched with headers from bytes still in the source. Both must remain unconsumed as body data after Host rejection.

Run all tests:

```sh
go test ./...
```

For a fresh verbose run:

```sh
go test -v -count=1 ./...
```

Run one test by exact name:

```sh
go test ./tcp/http/server -run "^TestHeaderValueParsing$" -v
```

In Windows Command Prompt, single quotes become literal characters and can cause the misleading result `no tests to run`. Double quotes avoid that issue.

The complete suite passed after the latest header-value validation change. These tests document the behavior implemented so far; they do not establish full HTTP compliance or production readiness.
