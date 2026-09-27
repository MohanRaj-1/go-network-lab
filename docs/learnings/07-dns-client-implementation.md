# DNS Client Implementation

This document records how I designed and implemented the Go DNS client. [DNS Concepts and Wire Format](06-dns-concepts-and-wire-format.md) explains the protocol and captured bytes; this document focuses on code organization, ownership, failure boundaries, and the decisions that make the implementation testable.

## 1. Design and Package Structure

### Design Goals

The goal was to build one complete IPv4 lookup path from small components whose behavior could be checked independently. The client needed to:

- Construct a DNS question for a name with type A and class IN, then encode it into wire bytes.
- Send the query to an explicitly supplied resolver over UDP.
- Decode the response with bounds checks before reading binary fields.
- Verify that the decoded response matches the transaction ID and question sent.
- Fall back to TCP when a decoded and validated UDP response has the truncation flag set.
- Extract matching IPv4 addresses while rejecting malformed matching A records.
- Apply one overall deadline and respond to caller cancellation during network I/O.
- Allow parsing, validation, extraction, framing, and transport behavior to be tested separately.

The public lookup operation is `LookupA(ctx, server, name) ([]net.IP, error)`. It constructs the A/IN question internally, so callers cannot accidentally request an AAAA lookup through an IPv4-specific API. Lower-level functions continue to accept `Question` and `Message` values because they serve different parts of the pipeline.

### Package Structure

The implementation and its command-line entry point currently live under `internal/dns`:

```text
internal/dns/
├── client/
│   └── main.go
├── constants.go
├── message.go
├── encode.go
├── decode.go
├── name.go
├── question.go
├── record.go
├── validate.go
├── rcode.go
├── extract.go
├── lookup.go
├── tcp.go
└── *_test.go
```

The `dns` package contains the reusable implementation. The `client` directory is a separate `main` package that imports it. Go's `internal` directory rule keeps this implementation within the permitted parent tree rather than exposing it as an unrestricted external library.

| File | Responsibility |
| --- | --- |
| `constants.go` | Names the record types, class, flag bits, and RCODE mask used by this phase |
| `message.go` | Defines `Header`, `Question`, `ResourceRecord`, and `Message` as decoded data structures |
| `encode.go` | Encodes labels, a question, and a header into the query returned by `EncodeQuery` |
| `decode.go` | Decodes the header, exposes flag helpers, and orchestrates all declared sections in `DecodeMessage` |
| `name.go` | Decodes names and compression pointers while preserving the caller's continuation offset |
| `question.go` | Combines name decoding with QTYPE and QCLASS reads |
| `record.go` | Decodes resource records, copies raw RDATA, and provides the repeated-record loop |
| `validate.go` | Checks response/query correlation and compares question names using ASCII case folding |
| `rcode.go` | Converts nonzero response codes into a typed `DNSResponseError` |
| `extract.go` | Selects matching A/IN answers and interprets their RDATA as IPv4 addresses |
| `lookup.go` | Owns the complete lookup: transaction ID, UDP connection, receive loop, timeout, fallback, and processing order |
| `tcp.go` | Exchanges one length-prefixed query and response over TCP using complete writes and exact reads |
| `client/main.go` | Supplies the example name and resolver, connects Ctrl+C to cancellation, and prints results or errors |

The command-line program does not duplicate the socket or decoder logic. Calling `LookupA` gives it the same behavior exercised by the package's integration tests. The lookup implementation owns transport decisions; the entry point owns presentation and process exit behavior.

Tests sit beside the code they exercise. Encoding, decoding, name handling, validation, RCODE, and extraction have focused tests. `message_test.go` joins the decoding pieces, `lookup_test.go` uses local UDP servers, `tcp_test.go` checks framed I/O, and `fallback_test.go` exercises the transition between transports. This organization lets a failure be investigated at the relevant layer before testing the entire lookup again.

### Layering Principle

The experiments led to a separation based on what each function needs to know:

```text
LookupA: coordinates one lookup
    |
    +-- Transport: UDP / TCP / deadlines / cancellation
    |
    +-- TCP framing: length prefix / complete writes / exact reads
    |
    +-- Message parsing: header / questions / records / compression
    |
    +-- Response validation: does this message match our query?
    |
    +-- Result handling: RCODE, then A-record interpretation
```

`exchangeTCP` does not interpret DNS fields. It adds the length prefix to the query and reads a complete response payload. Its one payload-size check requires enough bytes for a DNS header, but it does not inspect the ID, flags, questions, or answers.

`DecodeMessage` does not know whether its input came from UDP, TCP, or a test fixture. For TCP, the framing layer removes the length prefix before the DNS bytes reach the decoder. This also keeps compression offsets relative to the DNS message rather than the surrounding transport frame.

Successful parsing does not establish that a response belongs to our query. `ValidateResponse` receives the decoded message and the expected query details, while `validateRCode` interprets the server's result separately. `ExtractARecords` then interprets only matching A/IN data. Keeping RDATA raw in the generic decoder avoids coupling every record read to IPv4-specific rules.

These boundaries determine how errors travel as well. A short TCP body is a framing/I/O failure, an incomplete declared DNS record is a decoding failure, a wrong question is a validation failure, and malformed matching A data is an extraction failure. The lookup decides whether a particular failure ends the operation or, for an unrelated UDP response, means it should keep waiting under the same deadline.

The same separation makes testing practical. A decoder test can supply bytes without opening a socket. An extraction test can construct records directly. A framing test can send an opaque payload without constructing a meaningful DNS answer. Only the integration tests need to combine these pieces into a complete query and response exchange.

## 2. Query Construction

`LookupA` starts with a name and constructs the question itself:

```go
question := Question{Name: name, Type: TypeA, Class: ClassIN}
```

This fixes the operation's meaning at the API boundary: request IPv4 address records in the Internet class. The caller supplies the name and resolver address, while the lookup owns the transaction ID and query bytes.

```text
LookupA(ctx, server, name)
    |
Question{Name: name, Type: TypeA, Class: ClassIN}
    |
Random 16-bit transaction ID
    |
EncodeQuery(id, question)
    |
12-byte header + encoded QNAME + QTYPE + QCLASS
    |
Complete DNS query bytes
```

### A Transaction ID for Each Lookup

`LookupA` reads two bytes from `crypto/rand` and interprets them as a big-endian `uint16`. It generates the ID once per lookup rather than reusing the fixed `0x1234` value from the early packet experiments. If random-byte generation fails, the lookup returns an error before sending anything.

The ID lets the client correlate a response with the outstanding query. Random generation makes the value less predictable, but a 16-bit value is not guaranteed to be unique and is not proof that a response is trustworthy. The receive path also validates QR and the returned question.

`EncodeQuery` takes the ID as an argument instead of generating it internally. That keeps encoding deterministic for tests: a known ID and question produce known bytes. The lookup owns ID generation because it also needs the same value when validating responses.

### Constructing the Header

`EncodeQuery` builds a `Header` and passes it to `encodeHeader`, which writes six two-byte fields into a 12-byte buffer using `binary.BigEndian.PutUint16`.

| Field | Value in our query | Reason |
| --- | --- | --- |
| ID | Generated lookup ID | Correlate the response with this query |
| Flags | `FlagRD`, or `0x0100` | Ask the configured resolver to perform recursive resolution |
| QDCOUNT | `1` | Exactly one question is appended |
| ANCOUNT | `0` | The query includes no answer records |
| NSCOUNT | `0` | The query includes no authority records |
| ARCOUNT | `0` | The query includes no additional records |

Setting RD expresses the client's request for recursion; it does not guarantee that the server will offer or successfully perform it. Our client delegates resolution to the configured resolver rather than following referrals itself. The header's other flag bits remain clear, including QR, because this is a query.

The counts describe the sections actually encoded. In particular, zero answer records means we are asking a question without supplying answers; it does not predict how many answers the response will contain. No EDNS record is appended, so the additional count is also zero.

### Encoding the Name

`encodeQuestion` calls `encodeName` before writing QTYPE and QCLASS. The name encoder removes one optional trailing dot, splits the name into labels, and writes each label's byte length followed by its bytes. It appends a zero byte to terminate the name.

For `example.com`:

```text
Text:   example.com
Labels: "example", "com"
Wire:   07 example 03 com 00
Hex:    07 65 78 61 6d 70 6c 65 03 63 6f 6d 00
```

The output contains 13 bytes. The separator dot is represented by the boundary between labels rather than copied into the wire name. `example.com.` produces the same encoding. The explicit root name `.` produces a single zero byte, while an empty input string is rejected.

The encoder checks the lengths in bytes, not Unicode characters:

- Each label must contain at most 63 bytes.
- The complete encoded name, including length bytes and the root terminator, must contain at most 255 bytes.
- Empty labels inside the name are rejected, so inputs such as `example..com` or `.example.com` do not produce a packet.

These are the encoder's supported structural checks, not a general hostname or internationalized-name validation service. The implementation does not perform Unicode normalization or IDNA conversion. It encodes ordinary labels directly and does not compress query names.

Rejecting invalid input at this stage prevents the client from sending a malformed name and then waiting for a timeout or server error. `encodeQuestion` propagates name errors, `EncodeQuery` returns them, and `LookupA` reports an encoding failure before opening the UDP connection.

### Appending Type and Class

After the encoded name, `encodeQuestion` reserves four bytes and writes QTYPE and QCLASS as big-endian values. For our A/IN question, both values are `1`:

```text
QNAME:  07 65 78 61 6d 70 6c 65 03 63 6f 6d 00
QTYPE:  00 01
QCLASS: 00 01
```

The question occupies 17 bytes. `EncodeQuery` concatenates its encoded header and question to form the final 29-byte message. With the illustrative ID `0x1234`, that message is:

```text
12 34 01 00 00 01 00 00 00 00 00 00
07 65 78 61 6d 70 6c 65 03 63 6f 6d 00
00 01 00 01
```

### Reusing the Query for TCP

The lookup retains this byte slice after sending it over UDP. If a validated truncated response triggers fallback, it passes the exact same slice to `exchangeTCP`. It does not generate another ID or encode another question.

For the 29-byte example, TCP sends:

```text
00 1d | original 29-byte DNS query
```

Only the two-byte transport length prefix is added. Keeping one encoded query for both transports makes the fallback part of the same lookup and preserves the values expected by response validation. The fallback integration test compares the received UDP query with the TCP payload byte for byte.

## 3. DNS Message Decoding

`DecodeMessage(data []byte) (Message, error)` turns a complete DNS payload into the package's data structures. It receives no connection or context: the transport layer has already obtained the bytes and, for TCP, removed the length prefix.

```text
[]byte
    |
decodeHeader
    |
decodeQuestion, repeated QDCOUNT times
    |
decodeRecords
    +-- ANCOUNT records -> Answers
    +-- NSCOUNT records -> Authority
    +-- ARCOUNT records -> Additional
    |
Message
```

The pipeline separates orchestration from individual field reads. `DecodeMessage` decides which section comes next; the lower-level decoders determine how many bytes each item occupies and whether it can be read safely.

### Start with the Fixed Header

The header always occupies the first 12 bytes. `decodeHeader` checks that those bytes exist before reading its six two-byte fields with `binary.BigEndian.Uint16`. A shorter input immediately returns an error.

The header provides the counts needed to parse the remaining sections. After successfully decoding it, `DecodeMessage` starts the running offset at `12`. This is the only section boundary it can establish from a fixed header size alone.

| Header field | Decoding action |
| --- | --- |
| QDCOUNT | Call `decodeQuestion` that many times |
| ANCOUNT | Decode that many answer records |
| NSCOUNT | Decode that many authority records |
| ARCOUNT | Decode that many additional records |

A zero count skips the corresponding section without advancing the offset. The generic decoder does not require one question or any answers; the single-question rule belongs to response validation for our client.

### Advance Using Returned Offsets

Names and record data have variable lengths, so a decoder cannot locate the next item by assuming a fixed question or record size. Each item decoder returns its value, the next unread position, and an error:

```go
question, next, err := decodeQuestion(data, offset)
```

After checking the error, the caller appends the item and assigns `offset = next`. `decodeRecords` applies the same pattern to resource records and returns the position after the entire section.

For the captured response, this produces:

```text
Header          0 -> 12
Question       12 -> 29
First answer   29 -> 45
Second answer  45 -> 61
Empty sections 61 -> 61
```

Those values are consequences of this packet's encoding, not constants in the parser. A different name length or RDLENGTH changes the resulting offsets. The decoder also keeps the original message slice available to each helper, because compression pointers refer to offsets from the beginning of that message.

### Check Bounds Before Reading

Network bytes are input, not a guarantee that the declared fields exist. Slicing beyond a Go slice's bounds would panic, and reading fields before checking their lengths would make malformed packets a control-flow problem rather than a reported parsing error.

Each helper therefore establishes the bounds of the next read. `decodeQuestion` checks for two QTYPE bytes and then two QCLASS bytes. `decodeResourceRecord` checks TYPE, CLASS, the four-byte TTL, and RDLENGTH individually. It then verifies that the declared number of RDATA bytes remains before copying them.

Name decoding checks its starting and current positions, the second byte of every pointer, and the bytes required by each label. Errors identify the failed field or boundary, while callers add context such as the question index or record section.

### Decode Labels and Preserve the Continuation Position

`decodeName` uses a `current` position to inspect the name encoding. For an ordinary label, it reads the length byte, checks the label length and available bytes, appends the label, and advances past it. A zero byte ends the name. The labels are joined with dots to form the returned string.

The decoder supports ordinary labels and compression pointers, rejects other prefixes, and enforces the expanded name-length limit. These checks are limited to the name formats implemented in this phase.

A compression pointer changes where the remaining name is read. Its top two bits identify it, and the remaining 14 bits give the target offset. Following that target must not lose the caller's position after the original encoded name.

The implementation keeps those responsibilities separate:

```text
current     Where the decoder reads labels or pointers
nextOffset  Where the caller should continue after this name
```

`nextOffset` starts at `-1`. At the first pointer, the decoder saves `current + 2` there before following the target. Later pointers update `current` without replacing the saved continuation position. When the terminating zero is reached, the saved position is returned. If no pointer was followed, the continuation position is simply the byte after that zero.

For an answer whose NAME is `c0 0c` at offset `29`, the decoder reads labels at offset `12` but returns `31` to the resource-record decoder. TYPE is read at `31`, immediately after the pointer, rather than after the original name in the question section.

### Bound Pointer Traversal

A pointer can lead to another pointer, including a cycle such as a pointer back to itself. Bounds checks alone cannot stop a cycle whose targets all lie inside the message.

The decoder counts pointer jumps and permits at most 16. Attempting a seventeenth returns an error. This gives traversal a simple upper bound without allocating a visited-offset map.

The trade-off is deliberate: the limit rejects excessively long terminating chains as well as cycles. It does not attempt to identify which offset caused a cycle. Tests exercise both the allowed jump boundary and rejection beyond it.

### Preserve Raw Record Data

After decoding NAME and the fixed record fields, `decodeResourceRecord` uses RDLENGTH to locate the end of RDATA. It allocates a separate byte slice and copies the data into `ResourceRecord.RData`.

Copying makes the decoded record independent of the input buffer. Reusing or changing that buffer later cannot silently alter the retained RDATA. The cost is an allocation and a copy for each record.

The decoder does not convert those bytes into an IP address or follow a name encoded inside them. Its responsibility is the generic record layout. `ExtractARecords` later applies the A/IN-specific rules. This lets the message parser retain other record types without pretending to implement their semantics.

### Complete Declared Sections or Return an Error

Header counts control the loops, but they are not trusted as proof that sufficient bytes exist. If ANCOUNT is two and only one complete record is available, decoding the second fails and the entire `DecodeMessage` call returns `Message{}` with an error.

`decodeRecords` returns `nil` records and an error if any entry fails. `DecodeMessage` similarly discards questions and earlier sections on a later failure. Errors are wrapped with the section and one-based item index, making failures such as an incomplete second answer easier to locate.

The implementation grows slices as items decode rather than allocating capacity directly from the header's untrusted counts. A large count on a short packet reaches a bounds error; it does not justify allocating storage for thousands of records before reading them.

This complete-message-or-error contract prevents callers from mistaking a successfully decoded prefix for a valid complete response. A successful return means every declared item was decoded within our supported formats. It does not establish query matching or record-specific correctness. The current implementation also does not reject trailing bytes after the declared sections, so successful decoding alone is not a general guarantee that every input byte was consumed.

## 4. Response Validation and Result Interpretation

After decoding, the client still has three separate questions to answer: does this message match the query, did the server report an error, and which answers contain usable IPv4 addresses? Each question has its own function rather than being folded into the decoder.

```text
Decoded Message
    |
ValidateResponse(message, queryID, question)
    +-- ID, QR, QDCOUNT, decoded question count
    +-- Question name, type, and class
    |
LookupA handles TC and any TCP fallback
    |
validateRCode(message)
    |
ExtractARecords(message, question)
    |
[]net.IP
```

TC remains an orchestration decision in `LookupA`. If fallback occurs, the TCP response must be decoded and validated before it reaches RCODE handling and extraction.

### Three Functions, Three Contracts

| Function | Responsibility | Successful result |
| --- | --- | --- |
| `ValidateResponse` | Compare the decoded message with the expected ID and single question | `nil`: the response matches the query |
| `validateRCode` | Interpret the header's response code | `nil`: RCODE is NOERROR |
| `ExtractARecords` | Filter answer records and interpret matching A/IN data | IPv4 addresses, or an empty slice if none match |

`ValidateResponse` receives the expected query details explicitly. It checks both the declared and decoded question counts before accessing the question, then compares the name, type, and class. The name helper centralizes ASCII case-insensitive comparison and optional trailing-dot handling so validation and extraction use the same matching rule. A successful validation does not promise a successful lookup or require any answers.

`validateRCode` needs only the message. A nonzero code becomes `*DNSResponseError`, whose `Code` field lets callers distinguish outcomes using `errors.As`. It neither decides retry policy nor turns a server-reported error into a parsing error.

`ExtractARecords` needs the message and requested question, but no transaction ID or transport information. It skips unrelated records, verifies both RDATA lengths on matching A/IN records, and converts four bytes into an address. A malformed matching record returns an error and no partial result. Zero usable records returns an empty slice without an error.

### Why Keep the Boundaries Separate?

Each function operates on Go values without opening a socket or controlling a deadline. `LookupA` owns the order of operations and decides what to do with failures. For example, it can continue waiting after a UDP validation mismatch while returning a typed RCODE error from an accepted response.

This structure keeps changes local. Adding another record interpreter would not require changing transaction-ID validation. Changing retry policy would belong to the lookup orchestration rather than RCODE interpretation. The generic decoder can also parse messages independently of any outstanding query.

The tests mirror those contracts. Validation tests construct `Message` values and change one property, such as the ID or question type, without building wire packets. RCODE tests check numeric error preservation and `errors.As`, including wrapped errors. Extraction tests supply records directly to check filtering, length errors, empty results, and the rejection of partial results. Network integration tests then verify that `LookupA` connects these independently tested steps in the correct order.

## 5. UDP Lookup Pipeline

`LookupA` owns one lookup from the caller's context to the returned address slice. Its ordering matters because each step establishes what the next step may assume: bytes must be correlated before expensive parsing, a decoded message must match the question before its result is used, and truncation must be handled before accepting answers.

```text
Derive lookup context and check cancellation
    |
Construct A/IN question and generate transaction ID
    |
Encode query
    |
Dial connected UDP socket
    |
Set absolute deadline and send query
    |
Receive datagram
    |
Filter transaction ID
    |
DecodeMessage
    |
ValidateResponse
    |
TC?
    +-- Clear -> validateRCode -> ExtractARecords
    +-- Set   -> TCP fallback, then decode and validate again
```

### Establish the Operation Before Sending

The function derives a context with a three-second maximum duration from the caller's context and checks whether it is already canceled. It constructs the question, generates the random transaction ID, and encodes the query before opening a socket. Invalid name input or an ID-generation failure therefore ends the operation before a query is sent.

`net.Dialer.DialContext` opens a dedicated connected UDP socket for the supplied server address. Connected UDP establishes a peer for this socket; it does not add a TCP-style handshake or reliable delivery. The connection gives this lookup a single destination and a resource it can close independently of other lookups.

After dialing, the function arranges connection cleanup and cancellation handling, sets the absolute deadline for reads and writes, and sends the encoded query once. The receive loop does not retransmit the query each time it ignores a packet.

### Filter Before Full Decoding

The receive buffer is reused for successive datagrams, and only `buffer[:n]` is passed onward. Bytes beyond the current datagram's length are not part of its message.

The first two DNS bytes contain the transaction ID. `LookupA` first checks that those bytes exist, then compares their big-endian value with the generated ID. A packet shorter than two bytes cannot be correlated and is skipped. A wrong-ID packet is also skipped without attempting `DecodeMessage`.

This ordering avoids parsing a packet already known to be unrelated. It also means a malformed wrong-ID packet cannot terminate the lookup merely because its remaining bytes cannot be decoded. The connected peer alone is not enough to establish that a datagram answers this particular query.

### Decide Whether to Continue or Fail

A matching ID makes the packet a candidate response, not a proven valid answer. The next step is full decoding, followed by comparison with the expected question.

| Receive-loop outcome | Action |
| --- | --- |
| Fewer than two bytes or wrong ID | Continue waiting |
| Matching ID but decoding fails | Return a decoder error |
| Decoding succeeds but response validation fails | Continue waiting |
| Decoding and validation succeed | Process TC, then the server result |

Returning an error for a matching-ID malformed packet is an explicit policy in this implementation. Once a candidate with our ID cannot be parsed, the client reports that failure instead of silently converting it into a later timeout. The ID is not authentication; this policy does not claim that matching bytes prove the sender produced a legitimate answer.

A decoded packet with QR unset, a wrong question, or an unexpected question count does not satisfy the lookup's response contract. On UDP, the client can discard that datagram and wait for another one. `ValidateResponse` reports the mismatch, while `LookupA` decides that its transport policy is to continue. If no acceptable response follows, the existing deadline eventually ends the operation.

### Handle Truncation Before Results

After validation, TC determines whether the UDP message can be used as the final response. If TC is clear, the client checks RCODE and extracts matching addresses. If TC is set, it closes the UDP socket and passes the same query bytes, server, and context to `exchangeTCP`.

The TCP response replaces the UDP message only after successful decoding and validation. A still-truncated TCP response is an error; otherwise it reaches the same RCODE and extraction steps. This prevents addresses or an RCODE in the truncated UDP response from being treated as the final lookup outcome.

The current ordering also has a known limit: UDP decoding must succeed before TC is inspected. An incomplete declared record produces a decoder error even when the header has TC set. That behavior follows from the strict decoding contract and is documented rather than hidden by the orchestration.

### Keep One Deadline Active

The child context establishes the overall budget near the beginning of the lookup. An earlier caller deadline takes precedence. `conn.SetDeadline` applies the resulting absolute time to both the UDP send and receive operations; the receive loop does not move it forward.

Ignoring a packet must not buy the lookup more time. Otherwise, repeated unrelated responses could keep it alive indefinitely. TCP fallback receives the same context and applies its remaining deadline rather than starting another timeout.

Cancellation needs more than a deadline. A caller might cancel while `conn.Read` is blocked well before that deadline. `context.AfterFunc` registers a callback that closes the socket when the context becomes done, causing blocked network I/O to return. The code checks the context around receives and uses `lookupIOError` to preserve cancellation or deadline errors when reporting an I/O failure. Deferred cleanup closes the connection and stops the callback if it has not started.

### Why Orchestration Belongs in LookupA

The parser cannot decide whether to ignore a UDP datagram, open a TCP connection, or wait longer. It has neither the expected query details nor ownership of a connection or time budget. Its job is to interpret the bytes supplied to it and report structural failures.

`LookupA` has the information needed for those decisions: the caller's context, server address, generated ID, encoded query, and expected question. Keeping the receive loop and fallback policy here lets the lower-level functions remain independently testable while the lookup tests exercise their ordering under real local socket I/O.

## 6. TCP Fallback and Framing

TCP gives us a reliable, ordered byte stream, but it does not preserve application-level message boundaries. Switching transports therefore requires more than replacing a UDP dial with a TCP dial: the client must write a complete frame and use the response's length prefix to determine exactly where the DNS message ends.

```text
Decoded and validated UDP response has TC=1
    |
exchangeTCP(ctx, server, query)
    |
Check query size and dial TCP
    |
Build [2-byte query length][query bytes]
    |
writeFull
    |
io.ReadFull: 2-byte response length
    |
Check response length, then io.ReadFull: payload
    |
Complete DNS payload without prefix
    |
DecodeMessage
    |
ValidateResponse
    |
Reject TC if still set; otherwise handle RCODE and extract addresses
```

### Preserve the Lookup Across Transports

`LookupA` finishes the UDP exchange and passes the existing query slice to `exchangeTCP`. The question has not changed, so there is no reason to encode it again or generate another transaction ID. Reusing the bytes preserves the exact query that response validation expects.

`exchangeTCP` owns dialing and closing the TCP connection. It delegates framed I/O to `exchangeTCPConn`, which can also be tested using an established connection such as `net.Pipe`. This keeps the network setup separate from the read/write behavior without moving DNS interpretation into the transport helper.

### Frame the Query Without Changing Its DNS Bytes

The TCP frame contains an unsigned, big-endian two-byte length followed by that many DNS bytes. The prefix does not count itself. A 29-byte query therefore becomes:

```text
00 1d | original 29-byte DNS query
```

Before converting the query length to `uint16`, the helper rejects lengths above 65535. Checking after conversion would be too late because the conversion can discard high bits. The established-connection helper repeats this check deliberately so direct callers receive the same protection.

The implementation allocates one frame buffer, writes the prefix into its first two bytes, and copies the query after it. The prefix belongs to the transport layer: it is never passed to `DecodeMessage`. This also keeps DNS compression pointers relative to the start of the DNS message rather than two bytes into a TCP frame.

### Write and Read Complete Frames

`writeFull` tracks the bytes remaining and advances by the number reported by each write. It returns a write error immediately and treats a zero-byte write without an error as `io.ErrShortWrite`, preventing a loop that makes no progress. It also handles a writer that accepts only part of the buffer before the next call.

On receive, neither a prefix nor a payload is guaranteed to arrive in one read. `io.ReadFull` first fills a two-byte prefix buffer, then fills a payload buffer of the decoded length. TCP can split a field across reads or combine bytes from several server writes; neither behavior changes the message boundary supplied by the prefix.

For a 61-byte payload, reads of 8, 13, 4, and 36 bytes still produce the same complete slice. The decoder sees that slice only after framed I/O succeeds. An EOF, timeout, or other error before completion returns no partial payload.

The response length is checked before allocating and reading its body. Values below 12 cannot contain the fixed DNS header and are rejected at this boundary. This minimum-size check does not interpret DNS fields; full structural decoding still happens afterward.

### Share the Deadline and Interrupt Blocked I/O

Fallback uses the existing lookup context. `DialContext` observes it during connection setup, and `exchangeTCPConn` applies its absolute deadline to reads and writes. The helper does not create a fresh timeout or reset the deadline after reading the prefix.

If UDP has used two seconds of a three-second lookup budget, TCP has roughly one second left. A caller's earlier deadline remains effective across both transports.

To handle cancellation before the deadline, `context.AfterFunc` closes the connection when the context becomes done. That wakes blocked socket reads or writes. `lookupIOError` preserves context cancellation and deadline errors through wrapping, while other I/O errors retain their underlying cause. A final context check after reading the payload avoids reporting success when cancellation is already observable at that point.

### Validate the New Response

Successful framing means a complete payload arrived, not that it answers our question. Back in `LookupA`, the TCP bytes go through `DecodeMessage` and `ValidateResponse` using the original ID and question. Unlike the UDP receive loop, this dedicated one-query TCP exchange returns a validation error instead of waiting for another message.

If the validated TCP response still has TC set, the client returns an error. It does not recursively start another fallback, which would risk an unbounded sequence of TCP exchanges. Only a nontruncated response proceeds to RCODE handling and A-record extraction.

### What the Tests Establish

The tests separate framing behavior from DNS semantics and then verify their integration:

| Test area | Evidence |
| --- | --- |
| Response sent in pieces | The server writes the prefix bytes separately and the payload in several pieces; the client returns the exact complete payload |
| Short writes | A custom writer accepts at most two bytes per call; `writeFull` produces the complete output and rejects zero progress |
| Frame boundaries | Premature closes during prefix/body reads return errors with no payload; lengths below 12 fail and exactly 12 succeeds |
| Query-size boundary | 65536-byte queries fail before dialing; a 65535-byte query arrives unchanged |
| Cancellation | Cancellation during the exchange closes the connection and remains detectable as `context.Canceled` |
| Read deadlines | A silent server or a partially sent body leads to `context.DeadlineExceeded` |
| Write deadline | `net.Pipe` with no reader blocks the write until the existing deadline expires |
| Fallback integration | The server compares UDP and TCP query bytes, returns a different TCP answer, and the client uses that answer |
| Shared time budget | The server delays UDP before fallback; the lookup still ends within its original budget plus test scheduling tolerance |

Separate server writes do not guarantee separate client reads: TCP may combine them. The fragmented-response test exercises a server that sends in pieces, but does not prove every possible segmentation pattern. Exact prefix/body reads are enforced by the use of `io.ReadFull`; incomplete-frame tests check that the client does not accept a short payload. The custom writer and `net.Pipe` tests provide deterministic coverage for partial-write handling and blocked writes without relying on operating-system buffering behavior.

## 7. Testing Strategy

The test suite follows the boundaries of the implementation. Small tests establish what each component accepts, rejects, and returns on failure. Integration tests then check that the lookup connects those components in the right order. This makes a failure easier to locate than relying only on a live lookup that either prints an address or times out.

### Unit Tests and Integration Tests

| Test layer | Inputs and dependencies | What it checks |
| --- | --- | --- |
| Encoding and parsing units | Explicit byte slices, names, and field values | Wire representation, bounds checks, and returned offsets |
| Validation and interpretation units | Constructed `Message` and `ResourceRecord` values | Query matching, typed RCODE errors, filtering, and address conversion |
| Message decoding integration | Complete DNS payloads passed to `DecodeMessage` | Header, question, and record decoders working together across sections |
| Transport integration | Local UDP or TCP listeners | Real socket exchange, framing, filtering, and I/O failures |
| Fallback integration | UDP and TCP servers sharing a local port | One lookup preserving its query and deadline across transports |

Integration does not always require networking. `message_test.go` integrates the parsing helpers entirely in memory. Transport tests add local sockets only when the behavior under test depends on a connection or datagram exchange.

The fallback tests sit at the boundary between transport and orchestration: they use real local UDP/TCP sockets, but assert DNS-level behavior such as query preservation, response validation, and fallback decisions. Those orchestration rules are distinct from the mechanics of reading and writing sockets.

### Test Parsing Without a Resolver

Parser tests supply byte slices directly. They can choose exact label lengths, pointer targets, counts, and record fields without depending on a DNS server to produce those combinations. Explicit expected values also avoid making an encoder/decoder round trip the only source of evidence: matching mistakes in both components could otherwise cancel each other out.

Tests check continuation offsets as well as decoded values. A compression pointer can produce the correct name while returning the wrong position to the caller; the next field would then be read incorrectly. Name, question, and record tests therefore verify where decoding resumes, while complete-message tests check section ordering.

Constructed Go values exercise boundaries that wire decoding normally enforces. For example, extraction tests can supply a record whose `RDataLen` disagrees with `len(RData)`, and validation tests can construct a `Message` whose declared question count does not satisfy the validation contract, or whose decoded question slice does not contain the expected question. These cases check that exported data structures do not make later functions unsafe when used independently.

### Captured Responses as Fixed Fixtures

The captured 61-byte response provides a concrete integration fixture. `TestDecodeMessageCapturedResponse` checks the complete decoded header, echoed question, and two A records. Question and record tests also use captured bytes to verify offsets and individual fields.

These are snapshots, not live expectations about `example.com`. Their TTLs, addresses, and ordering are fixed test input. A future resolver response may differ without invalidating the fixture. The separate command-line experiment demonstrates a real resolver exchange; ordinary tests do not require that resolver or depend on public DNS availability.

### Control Transport Behavior Locally

The UDP helper listens on loopback with an operating-system-selected port. Each test supplies the server's reply behavior. It can send a short packet, an unrelated ID, a wrong question, and then a valid response, allowing the test to check that filtering continues instead of accepting the wrong data or failing too early.

The TCP helper accepts one connection, reads the length-prefixed query, and lets each test choose what bytes to send or when to close. This makes incomplete prefixes, short bodies, and invalid lengths reproducible. The framing success test uses an opaque payload so it can check byte preservation without depending on DNS interpretation.

`net.Pipe` supplies two connected in-memory endpoints for the blocked-write test. With no reader at the other endpoint, a write cannot simply disappear into a TCP socket buffer; it blocks until cancellation or the configured deadline interrupts it. A separate custom writer accepts at most two bytes per call to exercise `writeFull` and returns zero progress to test its failure path.

The helpers register cleanup to close listeners or sockets and wait for server goroutines. Server-side deadlines also bound test I/O. Resource cleanup is part of keeping failures diagnosable rather than leaving a blocked test server behind.

### Verify the UDP-to-TCP Transition

`fallbackTestServer` serves both transports on the same loopback port. It saves the UDP query and compares it byte for byte with the TCP payload after removing the framing prefix. That comparison includes the transaction ID, flags, and question.

Its UDP response sets TC and includes an address and RCODE that must not become the final result. The TCP response supplies a different address. The success test requires the TCP address with no error, demonstrating that the lookup used fallback rather than the truncated UDP result.

Other fallback cases return a wrong ID, wrong question, malformed message, nonzero RCODE, malformed matching A data, or another TC flag over TCP. They check that the new transport does not bypass decoding, validation, or interpretation, and that another truncated response does not trigger repeated fallback.

### Exercise Failure Boundaries

Malformed-input tests cover short headers, incomplete labels and pointers, out-of-bounds pointer targets, pointer cycles, excessive jumps, truncated record fields, and incomplete declared sections. Boundary cases include the permitted name length, the jump limit, the maximum query size, and the minimum TCP response length.

The assertions check more than the existence of an error. For decoding failures, they verify that no partially decoded `Message` is returned. Depending on the function's contract, they also check that records or addresses are not returned on failure, that a numeric RCODE is preserved, that a continuation offset is correct, or that an underlying error is discoverable through `errors.Is` or `errors.As`.

This protects the distinction between unrelated input and invalid matching data. An unrelated record is skipped; a malformed matching A record is an error. A wrong-ID UDP packet is ignored; a matching-ID decoding failure ends the lookup. A failure after one successful item must not expose that item as a partial successful result.

### Cancellation and Deadlines

Cancellation tests arrange for the server to cancel the caller's context after receiving the query. They check that the operation returns an error matching `context.Canceled`. Deadline tests leave a server silent or stop partway through a response and expect `context.DeadlineExceeded`. The pipe test covers a blocked write rather than only reads.

The fallback timeout test deliberately spends one second on UDP before leaving TCP waiting. It checks that the lookup still uses the original three-second budget, allowing 750 milliseconds of scheduling tolerance. That margin belongs to the assertion; it does not extend the client's configured deadline.

Timing tests use real scheduling and are not exact clock measurements. Cancellation after the server receives a query can occur just before or during the client's next blocked read. The tests establish the observable cancellation result without guaranteeing the precise instruction where cancellation occurs.

### What the Suite Does Not Prove

The examples and boundary cases are finite; they do not establish full DNS conformance or cover every malformed input. The current suite does not include fuzzing, exhaustive pointer graphs, or load and performance testing.

Loopback servers do not reproduce every public-network condition, firewall policy, packet-loss pattern, or resolver implementation. Separate TCP writes may be combined into fewer client reads, so the fragmented-response test does not force every possible segmentation pattern. Local timeout tests are evidence for the implemented budget behavior, not proof of exact scheduling on every machine.

The suite also does not establish support for features outside this phase, such as CNAME following, DNSSEC, EDNS, caching, or general record interpretation. Passing tests demonstrate the contracts exercised by the current implementation, including its documented limitations.

From the repository root, the DNS package and client can be checked with:

```bash
go test ./internal/dns/...
```

For a fresh execution of the fallback tests rather than cached results:

```bash
go test ./internal/dns -run TestLookupATCPFallback -v -count=1
```

## 8. Limitations and Engineering Trade-offs

Phase 5 targets one complete operation: request A/IN records from an explicitly supplied resolver and return matching IPv4 addresses, with UDP transport, TCP fallback, and bounded network I/O. The implementation makes that path inspectable and testable. It does not aim to replace an operating system's resolver or provide every DNS capability.

Two categories matter here. A feature can be deliberately excluded from the milestone without undermining the chosen contract. A design limitation, by contrast, affects how the current implementation behaves on inputs or workloads it may encounter. Documenting both makes clear what an extension would add and what a hardening change would need to address.

### Capabilities Outside Phase 5 Scope

| Choice | Reason for the boundary | Consequence |
| --- | --- | --- |
| A/IN lookup only | Keep the public operation specific and follow one record interpretation from query to result | `LookupA` returns IPv4 addresses; the AAAA constant does not provide an IPv6 lookup API |
| No CNAME following | Alias resolution needs its own name-tracking, loop limits, and additional-query policy | A response containing an alias and addresses under its target name may yield no usable addresses in this client |
| No general resource-record interpretation | Separate generic wire decoding from type-specific meaning | Other records retain raw RDATA but do not become typed lookup results |
| No caching | Avoid adding storage, expiry tracking, and cache policy while establishing the exchange pipeline | Every call starts a new query; the upstream resolver may still use its own cache |
| No recursive resolver | Delegate referral traversal to the supplied resolver | The client does not walk root, TLD, and authoritative servers or manage delegations |
| No EDNS | Keep query construction and response-code handling within the basic message format | No OPT construction, advertised UDP payload-size negotiation, or extended RCODE interpretation |
| No DNSSEC | Signature and chain-of-trust validation are separate from query correlation | Matching the ID and question does not authenticate DNS data |
| No DoH or DoT | Focus transport work on ordinary UDP and DNS-over-TCP framing | This client provides no encrypted DNS transport |
| No resolver discovery | Make the destination explicit and reproducible | Callers provide the DNS server endpoint as `host:port`; the example uses a fixed server rather than selecting one from OS configuration |

These exclusions define the client API's reach. For example, preserving an unknown record's bytes is useful generic decoding, but it is not support for resolving that record type. Similarly, delegating to a recursive resolver does not make this client a recursive resolver itself.

### Current Design Limitations

| Design choice | Benefit in this implementation | Known limitation |
| --- | --- | --- |
| Fully decode UDP before checking TC | Reuse the strict message decoder and validate the question before fallback | A truncated response with an incomplete declared record fails decoding before TCP fallback can begin |
| No retransmission or server failover | Keep one query, one destination, and one overall budget easy to follow | A lost UDP exchange or unavailable resolver ends in an error rather than another attempt or another server |
| Dedicated connections per lookup | Give each operation clear ownership, cleanup, and cancellation behavior | Repeated lookups incur socket creation and, for fallback, TCP connection setup; there is no connection pooling |
| Fixed three-second maximum lookup budget | Bound network waiting with a simple default and permit earlier caller deadlines | Callers cannot request a longer lookup through the current API |
| Return only `[]net.IP` | Keep the result convenient for an IPv4 lookup | TTLs and other record metadata are unavailable through this result, limiting richer caller policies |

The strict UDP ordering is particularly important: TCP fallback is implemented, but it is not reached for every possible TC-marked packet. Supporting that case would require a deliberate decision about which header and question checks are sufficient to trigger fallback before full section decoding. It should not be described as already handled by the current tests.

Retries, failover, and pooling were intentionally deferred, but their absence also creates practical limits in the current design. Adding them would require more than another loop: attempts must share a defined budget, errors need a retry policy, and reused connections would need coordinated ownership so canceling one lookup does not close another lookup's connection.

### Parser and Ownership Trade-offs

The name decoder uses a 16-jump limit rather than tracking every previously visited pointer offset. That keeps traversal bounded with little bookkeeping, but it rejects long terminating chains as well as cycles. It is a safety rule for this implementation rather than a complete classification of pointer relationships.

The record decoder copies RDATA so decoded records do not depend on the receive buffer's lifetime or future contents. This simplifies ownership at the cost of allocations and copying. Section slices also grow as records decode instead of reserving capacity from untrusted header counts, favoring cautious allocation over minimizing slice growth.

`DecodeMessage` checks all declared entries but currently accepts bytes left after the declared sections. Name handling is limited to ordinary labels and compression pointers, with ASCII name comparison rather than a general internationalized-name conversion layer. These are concrete parser boundaries, separate from omitted lookup features such as caching.

### Intended Engineering Scope

The result is a learning DNS client with explicit contracts, bounds checks, cancellation, and tests around failure behavior. Passing the suite demonstrates the contracts exercised by this implementation; it does not establish production-grade resolver behavior. The suite does not demonstrate broad DNS interoperability, exhaustive malformed-input handling, sustained concurrency performance, or operation across all network environments.

Extending this project should preserve the existing boundaries: record interpretation belongs outside generic decoding, retry and server-selection policy belongs to lookup orchestration, and transport helpers should continue to return complete payloads or errors. Future milestones can then expand capability or harden a known limitation with a specific contract and tests, rather than implicitly changing what the current API promises.
