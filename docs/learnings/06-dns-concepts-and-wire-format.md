# DNS Concepts and Wire Format

This document records what I learned by building a DNS client in Go, inspecting real response bytes, and turning those bytes into questions, records, and usable IP addresses.

> DNS is a protocol. The bytes on the wire are the source of truth; our Go structures and functions are interpretations of those bytes.

## 1. Why DNS Exists

In the earlier TCP and HTTP experiments, making a connection required a destination address and port. A name such as `example.com` is useful to a person or an application, but IP packets need a destination IP address. DNS provides a way to look up the information associated with that name before connecting to the service.

Names also let applications refer to a service without hardcoding its current IP address. The name can stay the same while its address records change, and one name can have multiple addresses. DNS is therefore more than a fixed table with one IP address per name.

In this lab, I asked a DNS resolver for the `A` records of `example.com`. An `A` record carries an IPv4 address. One captured response contained two answers:

```text
example.com
    |
    | DNS lookup for A records
    v
172.66.147.243
104.20.23.154
```

These addresses are observations from that response, not permanent values to hardcode into the client. A later lookup can return different addresses or a different answer order.

Resolving the name and connecting to the destination are separate operations. The DNS exchange gives the client address information; the application then chooses an address and the appropriate service port, such as TCP port 443 for HTTPS. A successful lookup does not itself establish that connection or prove that the destination service is reachable.

DNS can provide other information through other record types, but this experiment starts with a concrete goal: send a question for an IPv4 address, read the response bytes, and determine which answers belong to that question.

## 2. DNS Hierarchy

A DNS name is made of labels separated by dots. For `www.example.com`, the hierarchy runs from the root on the right toward the more specific name on the left. Writing the name as `www.example.com.` makes the root explicit through the final dot.

```text
.                         Root
|
com                       Top-level domain (TLD)
|
example.com               Domain beneath com
|
www.example.com           Name beneath example.com
```

The root is the starting point of the DNS namespace. Root DNS servers can direct a resolver to the servers responsible for a top-level domain such as `com`; they do not need to hold the address of every host beneath it.

The `com` TLD is the next level. Its authoritative servers publish delegation information that directs a resolver toward the authoritative servers for a domain such as `example.com`.

The `example` label identifies a domain beneath `com`. The full domain name is `example.com`, not just `example`. Its DNS administrators manage records for names in their zone and can delegate parts of that namespace to other servers. A zone is the portion of the namespace served authoritatively together; its boundaries are determined by delegation.

The `www` label identifies a name beneath `example.com`. It is commonly used for a web service, but DNS does not give the word `www` special behavior. That name might have address records, or it might be an alias pointing to another name. It does not necessarily identify one physical machine.

### Resolving the Name

Our client asks a recursive resolver, such as `1.1.1.1`, for the address information. The resolver obtains the answer on the client's behalf. If it already has a usable cached answer, it can respond without walking the hierarchy again.

When the necessary information is not cached, the conceptual process is:

```text
Client asks recursive resolver: A records for www.example.com?
    |
    | Resolver follows referrals as needed:
    |
    +-- Root server: where are the com DNS servers?
    |
    +-- com server: where are the example.com DNS servers?
    |
    +-- example.com authoritative server: records for www.example.com?
    |
    v
Resolver returns the result to the client
```

The resolver makes these requests; the root server does not forward the client's lookup through the entire chain. Referrals tell the resolver which servers to ask next. Further requests may be needed for aliases, additional delegations, or server addresses.

The distinction between the two server roles is important: a **recursive resolver finds or obtains an answer for the client**, while an **authoritative server serves the authoritative records for its zone**. A resolver's cached copy does not make it authoritative for that zone.

In this lab, our Go client sends its question to a recursive resolver. It does not contact the root and TLD servers itself or implement the referral-following process. The packets we captured show the exchange between our client and the resolver, not every lookup the resolver may have performed behind it.

## 3. Caching and TTL

When I queried `example.com` more than once, the address records could stay the same while their TTL values changed. One response showed a TTL of `247`; a later response showed `125`. Understanding those numbers requires separating the information in a record from what the resolver does with it.

TTL means **Time To Live**. In a DNS resource record, it is a value measured in seconds that tells a cache how long the record may be reused before it needs fresh information. It is not a timeout for our lookup, and it does not describe how long the destination server or IP address will exist.

> TTL is a property supplied with the DNS record; caching is behavior performed by a resolver or cache.

### Reusing a Cached Answer

A recursive resolver can store records it obtains and reuse them for later queries while they remain valid in its cache. It does not need to ask the authoritative server for the same information on every request. This reduces repeated DNS traffic and lets the resolver answer more quickly.

For an ordinary cached answer, the resolver reports the remaining TTL rather than restarting the original TTL every time someone asks. Otherwise, repeated queries could keep an old record alive indefinitely.

```text
Remaining TTL when observed:     247 seconds
Time spent in the cache:         122 more seconds
Remaining TTL in a later answer: 125 seconds
```

This illustrates how the observed decrease could occur if both responses came from the same cache entry without a refresh. The two values alone do not prove that exactly 122 seconds passed between our queries: responses can come from different cache instances, and a resolver may refresh an entry between requests.

### When the TTL Expires

Under ordinary TTL-based caching, an expired entry can no longer be treated as a fresh cached answer. If the information is still needed, the resolver must obtain a fresh answer, potentially using other still-valid cached delegation information along the way. The new answer may contain the same address or a different one, with a newly supplied TTL.

Expiry does not mean the domain has disappeared or an existing connection must close. It means the cached DNS information has reached its reuse limit. Some resolvers have explicit policies for serving stale answers during failures; that is separate from the basic TTL behavior studied here.

### What Our Client Does

Our `decodeResourceRecord` function reads the four TTL bytes in big-endian order and stores the result in `ResourceRecord.TTL`. For example, `00 00 00 7d` becomes `125` seconds. Parsing this field does not create a cache or start an expiry timer.

The current `LookupA` implementation makes a new query to the configured resolver for every call. It does not retain records between lookups, record insertion times, or check expiry times. `ExtractARecords` returns only the matching IP addresses, so the public lookup result does not include their TTLs.

Caching was deliberately left outside this milestone so I could focus on wire encoding, parsing, response validation, transport framing, and cancellation. The recursive resolver we contact can still answer from its own cache, which explains why we can observe decreasing TTLs even though our Go client has no cache of its own.

## 4. DNS Transport — UDP and TCP

DNS commonly uses UDP port 53 for ordinary queries. In this lab, the client starts by sending its encoded question to `1.1.1.1:53` over UDP. The DNS message defines what the bytes mean; UDP carries those bytes between the client and resolver.

### UDP Carries Datagrams

UDP preserves datagram boundaries, but it does not guarantee delivery, ordering, or protection against duplicate delivery. Sending a query successfully does not guarantee that the resolver receives it or that its response reaches our client.

Our receive buffer holds one response datagram at a time. Before decoding a packet, `LookupA` checks whether it contains a transaction ID matching the query. Unrelated packets are ignored while the original lookup deadline continues to run.

A timeout is necessary because a response might never arrive. Our lookup has an overall three-second limit, or an earlier deadline supplied by the caller. Context cancellation can also stop it before that deadline. The current client does not automatically resend a UDP query when it times out.

### TCP Carries a Byte Stream

DNS can also use TCP port 53. TCP provides a reliable, ordered byte stream while the connection is functioning, but a connection can still fail or time out. It also does not preserve application message boundaries: one server write does not necessarily become one client read.

DNS-over-TCP identifies each message with a two-byte, unsigned, big-endian length prefix:

```text
[2-byte message length][DNS message bytes]
```

The length excludes the prefix itself. For a 61-byte DNS response, the frame begins with `00 3d`, followed by the 61 response bytes. The complete frame is 63 bytes long.

The client first uses `io.ReadFull` to obtain both prefix bytes. It decodes the length, checks that the response can contain at least the 12-byte DNS header, and then uses `io.ReadFull` again to read exactly the declared number of bytes.

Those bytes might arrive in reads of 8, 13, 4, and 36 bytes. A read can end halfway through a name, TTL, or address without affecting DNS parsing. `exchangeTCP` assembles the complete payload before returning it; `DecodeMessage` receives that payload without the TCP prefix. If the connection ends before the payload is complete, the exchange returns an error instead of a partial message.

### Truncation and TCP Fallback

The DNS header's `TC` flag indicates that the response is truncated. Our client treats a validated UDP response with `TC=1` as a reason to repeat the query over TCP, rather than using its answer section as the final lookup result.

```text
Encoded DNS query
    |
    +-- UDP: [DNS query bytes]
    |       |
    |       +-- Validated response has TC=1
    |                |
    +-- TCP: [2-byte length][same DNS query bytes]
```

`LookupA` passes the original encoded query to `exchangeTCP`. It does not generate another transaction ID or re-encode the question. TCP adds transport framing around the same DNS message. Both exchanges share the original context and deadline, so fallback does not receive a new three-second budget.

The TCP response is decoded and validated again before RCODE handling and address extraction. If it still has `TC` set, our client returns an error rather than repeatedly opening TCP connections. One current limitation is that the UDP message must pass full decoding before the TC check; an incomplete declared record therefore produces a decoder error before fallback can occur.

### A Timeout Is Not a DNS Error Response

A UDP timeout means that the client did not obtain an acceptable response before its deadline. It does not establish whether the name exists or why the exchange failed. The query or response could have been lost, the resolver could be unavailable, or the client could have received only unrelated responses.

A nonzero RCODE is different: the client received and validated a DNS response, and the server reported a DNS-level result such as `NXDOMAIN`, `SERVFAIL`, or `REFUSED`.

| Outcome | What the client knows | How our code reports it |
| --- | --- | --- |
| Lookup deadline expires | No acceptable result arrived within the time budget | An error matching `context.DeadlineExceeded` |
| Valid response with nonzero RCODE | The server explicitly reported a DNS-level error | `DNSResponseError` preserving the numeric code |
| Valid NOERROR response without matching A records | The response contains no usable IPv4 answers for this lookup | Empty address result, no error |

Keeping these outcomes separate lets callers distinguish a transport or timing problem from an explicit DNS response. In particular, a timeout must not be interpreted as `NXDOMAIN`.

## 5. DNS Message Wire Format

The most useful way to understand our decoder is to follow the actual bytes it reads. This captured response is 61 bytes long:

```text
12 34 81 80 00 01 00 02 00 00 00 00
07 65 78 61 6d 70 6c 65 03 63 6f 6d 00
00 01 00 01
c0 0c 00 01 00 01 00 00 00 7d 00 04 68 14 17 9a
c0 0c 00 01 00 01 00 00 00 7d 00 04 ac 42 93 f3
```

The line breaks above are for readability; they are not separators in the protocol. All offsets below are zero-based positions in the DNS message, excluding any DNS-over-TCP length prefix. Multi-byte numeric fields use big-endian order: the most significant byte comes first.

### Message Structure

A DNS message has five sections in this order:

```text
Header       Fixed 12 bytes, including section counts
Questions    QDCOUNT questions
Answers      ANCOUNT resource records
Authority    NSCOUNT resource records
Additional   ARCOUNT resource records
```

Only the header has a fixed overall size. Questions and resource records contain variable-length fields, so the decoder must read their contents to find where the next item begins. Answers, authority, and additional all use the resource-record layout.

In our capture, the counts specify one question, two answers, and no authority or additional records. Our `Message` struct represents those sections after decoding; the counts and field encodings in the packet determine how we populate it.

### The 12-Byte Header

Each header field occupies two bytes:

| Offsets | Field | Bytes | Interpretation |
| --- | --- | --- | --- |
| 0–1 | ID | `12 34` | Transaction ID `0x1234` |
| 2–3 | Flags | `81 80` | Flags word `0x8180` |
| 4–5 | QDCOUNT | `00 01` | One question |
| 6–7 | ANCOUNT | `00 02` | Two answer records |
| 8–9 | NSCOUNT | `00 00` | No authority records |
| 10–11 | ARCOUNT | `00 00` | No additional records |

`decodeHeader` reads these values with `binary.BigEndian.Uint16`. The ID lets the client correlate the response with its query. It is not an address or a record count.

Our flag helpers interpret the following parts of the flags word:

| Field | Mask | Meaning | Value in `0x8180` |
| --- | --- | --- | --- |
| QR | `0x8000` | Query (`0`) or response (`1`) | Response |
| RD | `0x0100` | Recursion desired | Set |
| RA | `0x0080` | Recursion available | Set |
| TC | `0x0200` | Message truncated | Clear |
| RCODE | `0x000f` | Response code in the low four bits | `0`, NOERROR |

RCODE is a four-bit value, not a single boolean flag. The decoder preserves the full flags word, but our client only interprets the fields needed for this experiment. Parsing a response code and deciding what it means for a lookup remain separate operations.

### The Question

A question consists of:

```text
QNAME     Variable-length encoded name
QTYPE     2 bytes
QCLASS    2 bytes
```

For this packet:

| Offsets | Field | Bytes | Interpretation |
| --- | --- | --- | --- |
| 12–24 | QNAME | `07 65 78 61 6d 70 6c 65 03 63 6f 6d 00` | `example.com` |
| 25–26 | QTYPE | `00 01` | A, an IPv4 address record |
| 27–28 | QCLASS | `00 01` | IN, the Internet class |

`decodeQuestion` calls `decodeName`, then reads QTYPE and QCLASS from the returned position. It checks that each field fits before reading it and returns offset `29`, immediately after QCLASS.

The package also defines `TypeAAAA = 28`, but the current `LookupA` API asks for A/IN and extracts IPv4 addresses only. Recognizing a type number does not mean the client implements its record-specific behavior.

### Domain-Name Encoding

The wire representation of `example.com` is not the text string with a dot between its labels. Each label starts with a one-byte length, followed by that many label bytes. A zero-length label terminates the name at the root:

```text
07  example  03  com  00
|   |        |   |    |
7   7 bytes  3   3    End of name
                 bytes
```

In hexadecimal:

```text
07                         Length of "example"
65 78 61 6d 70 6c 65       Bytes spelling "example"
03                         Length of "com"
63 6f 6d                   Bytes spelling "com"
00                         Root terminator
```

The encoded name occupies `1 + 7 + 1 + 3 + 1 = 13` bytes. The dots in the decoded string are added when the decoder joins the labels. A trailing dot in a textual name represents the root; it does not become a literal dot byte in this encoding.

For this milestone, our name decoder handles ordinary length-prefixed labels and compression pointers. It checks message bounds, rejects other label prefixes, and enforces the 63-byte label and 255-byte expanded wire-name limits. This is the supported scope of our implementation, not a claim to handle every DNS name representation.

### Resource Records

Each record in the answer, authority, or additional section has this layout:

```text
NAME       Variable-length encoded name, possibly compressed
TYPE       2 bytes
CLASS      2 bytes
TTL        4 bytes
RDLENGTH   2 bytes
RDATA      RDLENGTH bytes
```

The first answer starts at offset `29`:

| Offsets | Field | Bytes | Interpretation |
| --- | --- | --- | --- |
| 29–30 | NAME | `c0 0c` | Pointer to `example.com` |
| 31–32 | TYPE | `00 01` | A |
| 33–34 | CLASS | `00 01` | IN |
| 35–38 | TTL | `00 00 00 7d` | 125 seconds |
| 39–40 | RDLENGTH | `00 04` | Four RDATA bytes |
| 41–44 | RDATA | `68 14 17 9a` | IPv4 address `104.20.23.154` |

`decodeResourceRecord` reads TTL with `binary.BigEndian.Uint32`, checks that the declared RDATA fits, and copies those bytes into `ResourceRecord.RData`. It does not interpret them as an address. `ExtractARecords` performs that later, after checking the name, A type, IN class, and four-byte length.

The second answer occupies offsets `45–60`. It has the same name, type, class, TTL, and RDLENGTH, but its RDATA is `ac 42 93 f3`, representing `172.66.147.243`.

The wire format can also carry records such as CNAME. Our generic decoder preserves their raw RDATA, but the current client does not follow CNAME chains.

### Name Compression: `c0 0c`

Both answers refer to a name already present in the question. Instead of repeating its 13-byte encoding, each uses a two-byte compression pointer.

The top two bits identify a pointer:

```text
c0 0c = 11000000 00001100
        ^^
        Pointer marker: 11
```

Removing the marker leaves a 14-bit offset:

```text
((0xc0 & 0x3f) << 8) | 0x0c = 12
```

Offset `12` is the first byte after the fixed header. In this particular message it contains `07`, the first label length of `example.com`. The pointer is relative to the beginning of the DNS message, not to the current record or the TCP frame.

Following a pointer changes where the decoder reads the remaining name, but not where the caller resumes. For the first answer:

```text
Pointer begins at 29
    |
    +-- Follow target 12 to read example.com
    |
    +-- Return nextOffset 31, after the two pointer bytes
```

Returning `25`, the position after the original question name, would make the caller read the wrong fields. Our `decodeName` saves the position after the first pointer and keeps it even if more pointers are followed. A limit of 16 pointer jumps prevents cyclic pointer chains from hanging the decoder.

### Walking the Complete Packet

The returned offset from one decoder becomes the starting offset for the next:

| Stage | Start | Bytes consumed at that position | Next offset |
| --- | --- | --- | --- |
| Header | 0 | 12 | 12 |
| Question | 12 | 17 | 29 |
| First answer | 29 | 16 | 45 |
| Second answer | 45 | 16 | 61 |
| Authority and additional | 61 | 0 | 61 |

Each answer consumes `2 + 2 + 2 + 4 + 2 + 4 = 16` bytes. Reading the name through a pointer does not consume another copy of the pointed-to bytes in the record's sequential layout.

The final offset is `61`, equal to the captured message length. This is how the parser turns the packet into a `Message`: it follows the field sizes, counts, lengths, and pointers supplied by the bytes rather than assuming fixed positions for variable-length sections.

## 6. Response Validation

Successfully decoding bytes does not mean the response belongs to our query. `DecodeMessage` reads the declared sections and checks their structure within the formats we support. It has no knowledge of the transaction ID or question our client sent. That comparison belongs to `ValidateResponse`.

For our single-question lookup, validation follows this sequence and returns an error at the first failed check:

```text
Transaction ID matches the sent query
    |
QR = 1: message is a response
    |
QDCOUNT = 1
    |
Exactly one decoded question
    |
Question name matches
    |
Question type matches
    |
Question class matches
    |
Response matches our query
```

### Checking the Relationship to the Query

The transaction ID must match the ID generated for this lookup. For the earlier captured query, that was `0x1234`; the current `LookupA` generates a random 16-bit ID. QR must also be set: a packet containing the same question and ID with `QR=0` is still a query, not the response we expect.

We sent exactly one question, so the response header must declare `QDCount == 1`. We also check `len(message.Questions) == 1` before accessing the question. These normally agree after successful decoding, but `Message` can be constructed manually. The slice-length check makes the validator safe at that boundary and prevents an out-of-range access.

The returned question must match all three parts of the sent question: name, type, and class. For our lookup, that means `example.com / A / IN`. A response echoing `example.com / AAAA / IN` does not match merely because the name is correct.

Name comparison uses ASCII case-insensitive matching and removes one optional trailing dot from each name. That lets `EXAMPLE.COM`, `example.com`, and `example.com.` compare equally. Our helper does not apply general Unicode normalization or silently remove multiple trailing dots. Type and class are compared by their numeric values.

### A Matching ID Is Only the First Check

The first two bytes can match our query even when the rest of the packet is incomplete or unrelated to the question we asked. The ID is a correlation field, not proof that every other field is acceptable.

| Packet with a matching ID | Where our client handles it |
| --- | --- |
| Malformed name or incomplete declared record | `DecodeMessage` returns an error |
| QR is zero | `ValidateResponse` rejects it |
| Wrong question count, name, type, or class | `ValidateResponse` rejects it |
| TC is set on an otherwise decoded and validated response | `LookupA` requests TCP fallback for UDP, or rejects a still-truncated TCP response |
| Nonzero RCODE after truncation handling | `validateRCode` returns a typed `DNSResponseError` |

In the UDP receive loop, the ID check happens before full decoding so obviously unrelated packets can be skipped. A matching-ID decoder failure is returned as an error; a decoded packet that fails `ValidateResponse` is ignored while the client waits under the original deadline. On the dedicated TCP fallback connection, a validation failure returns an error rather than searching for another response.

### Why TC and RCODE Are Separate

Neither flag handling nor query matching should be confused with structural decoding. A message can be structurally decodable and match our question while still requiring another client action.

TC describes truncation. It does not by itself mean the response belongs to another query. After decoding and validation, our client checks TC before treating the answers as final or handling RCODE. A validated truncated UDP response causes TCP fallback using the same query; the TCP response is decoded and validated again. As noted earlier, our strict decoder can reject an incomplete UDP record before this TC check is reached.

RCODE describes the DNS server's result. For example, an NXDOMAIN response can be correctly encoded and match the query exactly, while reporting that the name does not exist. `ValidateResponse` therefore leaves it accepted as a matching response, and `validateRCode` reports its numeric code through `DNSResponseError`. NOERROR allows the client to proceed to address extraction; it does not guarantee that a matching A record exists.

The boundaries in our implementation are:

```text
DecodeMessage      Can we decode the declared message structure?
ValidateResponse   Does this response match the query we sent?
TC handling        Do we need TCP, or is the TCP response still truncated?
validateRCode      Did the server report a DNS-level error?
ExtractARecords    Which matching answers contain usable IPv4 addresses?
```

`ValidateResponse` also does not require a nonempty answer section. A response with zero answers can pass both decoding and query validation; the lookup can then return an empty address result without misclassifying it as malformed.

## 7. A Record Extraction

After decoding and response validation, we have resource records, but the caller of `LookupA` wants IPv4 addresses. `ExtractARecords` performs that final interpretation. It examines `message.Answers` and returns matching addresses as `[]net.IP`.

```text
ResourceRecord in the answer section
    |
TYPE = A, CLASS = IN, NAME matches the question
    |
RDataLen = 4 and len(RData) = 4
    |
Convert the four bytes with net.IPv4
    |
Append the address to the result
```

### Keeping Decoding Separate from Interpretation

`decodeResourceRecord` understands the common record layout: name, type, class, TTL, length, and data. It copies RDATA into a byte slice without interpreting its contents. Different record types give those bytes different meanings, so the generic decoder cannot assume every record contains an IPv4 address.

`ExtractARecords` supplies the interpretation for our A/IN lookup. It does not open sockets, check transaction IDs, decide on TCP fallback, or interpret RCODE. Those steps have already been handled by the lookup pipeline.

### Selecting Answers for Our Question

The extractor first checks that the supplied question is A/IN. For a different question type or class, it returns an empty result. `LookupA` constructs an A/IN question itself, but the extractor can also be called directly.

For each answer, it checks TYPE, CLASS, and NAME before examining the data length:

| Check | Reason |
| --- | --- |
| `record.Type == TypeA` | The four-byte IPv4 interpretation applies to A records |
| `record.Class == ClassIN` | Our lookup requests records in the Internet class |
| Record name matches the question name | An address for another name is not automatically an answer to this lookup |

The name check uses the same ASCII case-insensitive comparison and optional trailing-dot handling as response validation. Validating the echoed question does not remove the need to check each answer's owner name.

AAAA and CNAME records are skipped because their type is not A. Records for other names or classes are also skipped. The client does not follow CNAME chains, so an A record owned by an alias target is not accepted merely because it appears alongside a CNAME. The extractor reads only the answer section, not address records in authority or additional sections.

### Four Bytes Become an IPv4 Address

An IPv4 address has 32 bits, represented by four eight-bit octets. A matching A/IN record must therefore contain exactly four RDATA bytes. In our captured response:

```text
RDATA: 68 14 17 9a
       |  |  |  |
       104 20 23 154

Address: 104.20.23.154
```

The implementation checks both `record.RDataLen == 4` and `len(record.RData) == 4` before indexing the slice. The first is the length declared by the record; the second is the number of bytes actually available in the Go value. Our decoder normally keeps them consistent, but an exported `ResourceRecord` can be constructed manually.

`net.IPv4` constructs the address from those four bytes. The returned address does not share the record's RDATA storage, so changing that input slice later does not change the extracted address. The four-byte rule concerns the wire data; it does not require the resulting `net.IP` slice to have length four.

### Unrelated Records and Malformed Matches

An unrelated record is skipped without applying A-specific length rules. A malformed record that matches our requested name, type, and class is different: it claims to supply an IPv4 answer but cannot be interpreted as one.

For example, a matching A/IN record declaring four bytes but containing only two produces an error. Silently skipping it would hide invalid answer data and could make a broken response look like a successful lookup with fewer addresses.

The extractor follows the same complete-result-or-error convention as our decoder. If it collects one valid address and then encounters a malformed matching record, it returns `nil` and an error, discarding the collected address.

### No Usable A Records

If there are no answers, or every answer is unrelated to our A/IN question, the extractor returns an empty slice and no error. It does not invent an address, follow aliases, or classify the result as NXDOMAIN.

This preserves the distinction between a server-reported DNS error, malformed matching address data, and a valid response with no usable IPv4 records. Our command-line client handles the last case by printing `No matching IPv4 addresses found.`

## 8. Error Handling and Cancellation

Building the lookup meant deciding what each failure tells the caller, not just returning an error whenever something looks unusual. A packet for another query, an incomplete message, a server-reported DNS error, and a canceled operation need different handling.

```text
Encode query
    |
UDP transport and transaction-ID filtering
    |
Decode message
    |
Validate response against the query
    |
TCP fallback if needed: exchange, decode, validate
    |
Interpret RCODE
    |
Extract matching A records
```

### Failure Boundaries

| Stage or condition | Implemented behavior |
| --- | --- |
| Query encoding fails | Return an encoding error before sending; invalid names are not sent |
| Transaction-ID generation fails | Return an error rather than using a fallback ID |
| UDP dial, deadline setup, or send fails | Return an error identifying the operation |
| UDP packet has fewer than two bytes | Ignore it because it cannot be correlated by ID |
| UDP packet has the wrong transaction ID | Ignore it without fully decoding it; keep the original deadline |
| Matching-ID UDP packet is malformed | Return the decoder error |
| Decoded UDP packet has the wrong question, count, or QR | Ignore it and continue waiting under the original deadline |
| Lookup deadline expires | Return an error matching `context.DeadlineExceeded` |
| Caller cancels the context | Interrupt network I/O and return an error matching `context.Canceled` |
| TCP frame length is below 12 | Return a framing error before DNS decoding |
| TCP prefix or body is incomplete | Return the read error without a partial payload |
| TCP DNS message is malformed or mismatched | Return a decoding or validation error; do not search the stream for another answer |
| Validated TCP response still has TC set | Return an error; do not repeat fallback |
| Response has a nonzero RCODE after TC handling | Return `DNSResponseError` with the numeric code |
| Matching A/IN answer has invalid RDATA length | Return an extraction error without collected addresses |

Malformed packets include short headers, labels extending beyond the message, invalid or cyclic compression pointers, and sections whose declared entries cannot be fully read. Our decoder applies bounds checks before reading fields and limits pointer traversal. It returns a zero-value `Message` on failure instead of exposing a successfully decoded prefix as a complete response.

The UDP and TCP validation policies differ intentionally. UDP can deliver individual unrelated datagrams while the lookup waits. Our dedicated TCP connection carries one query and expects one response, so a mismatched TCP response ends the exchange with an error.

### One Deadline for the Whole Lookup

`LookupA` derives a context with a three-second timeout from the caller's context. An earlier caller deadline or cancellation takes precedence. The lookup applies the resulting absolute deadline to socket reads and writes and uses context-aware dialing.

Fallback receives that same context. If UDP has already used two seconds of the budget, TCP has approximately one second left. Ignored UDP packets and successful partial TCP reads do not restart the timer.

A socket deadline handles expiry, but cancellation can happen before that time. Both transports register a `context.AfterFunc` callback that closes the connection when the context becomes done. Closing the socket wakes a blocked read or write. The code stops the callback when the exchange finishes and closes connections on exit.

`lookupIOError` translates I/O failures caused by cancellation or deadline expiry into the corresponding context error. Other network errors remain available through wrapping with `%w`, so callers can use `errors.Is` rather than matching error text. The command-line entry point also connects Ctrl+C to context cancellation through `signal.NotifyContext`.

### TCP Framing Failures

Before sending, `exchangeTCP` rejects queries larger than 65535 bytes so converting the length to `uint16` cannot silently truncate it. `writeFull` sends the complete frame or returns an error; a writer making zero progress produces `io.ErrShortWrite`.

On receive, `io.ReadFull` reads the two-byte length and then the declared payload. A clean close before any bytes of the requested prefix or body arrive produces `io.EOF`; a close after only part arrives produces `io.ErrUnexpectedEOF`. Timeouts and other network failures are also propagated. None of these paths passes a partial payload to `DecodeMessage`.

### DNS Errors and Empty Answers

`DNSResponseError` preserves the server's RCODE. Callers can retrieve it with `errors.As` and distinguish NXDOMAIN from SERVFAIL or REFUSED without parsing strings. This helper does not decide whether to retry, and the client currently performs no automatic retries beyond its one UDP-to-TCP fallback.

A timeout means no acceptable result arrived within the budget. A DNS error means a response reported a nonzero code. A successful empty address result means the accepted NOERROR response had no usable matching A records. These outcomes remain distinct through the lookup API.

### No Partial Results on Errors

Each layer returns a complete result for its own contract or an error. `decodeRecords` discards records accumulated before a decoding failure. `DecodeMessage` discards an incomplete message. `exchangeTCP` discards an incomplete frame body. `ExtractARecords` discards collected addresses if a later matching record is malformed.

As a result, an errored lookup returns no addresses. Callers cannot accidentally use the first valid answer while overlooking that the rest of the response failed validation or extraction. An empty slice with no error remains a separate, intentional result.

## 9. Scope and Deliberate Limitations

This phase implements an A-record lookup client that asks a specified DNS resolver for IPv4 addresses. The scope was chosen to connect wire encoding, safe parsing, query validation, and real network I/O in one working path.

### What I Implemented

| Capability | Implementation scope |
| --- | --- |
| DNS query encoding | Single-question queries with a header, encoded labels, QTYPE, and QCLASS |
| Header and name decoding | Big-endian fields, ordinary labels, length checks, and root termination |
| Compression pointers | Pointer-target traversal, caller-offset preservation, and a 16-jump limit |
| Message decoding | Questions and generic resource records in answer, authority, and additional sections |
| A-record extraction | Matching A/IN answers converted from four RDATA bytes into `net.IP` values |
| Response validation | Transaction ID, QR, question counts, and matching name/type/class |
| UDP exchange | Dedicated connected socket, random transaction ID, and receive-loop filtering |
| TCP fallback | One fallback for a decoded and validated truncated UDP response, using the exact same query |
| TCP framing | Two-byte length prefix, complete writes, and exact-length reads |
| Timeouts and cancellation | One overall lookup deadline, context cancellation, and socket cleanup |
| Error reporting | Operation context, typed RCODE errors, and no partial results on failure |
| Tests | Parsing and validation cases, captured packets, local UDP/TCP exchanges, fallback, framing, and cancellation/deadline behavior |

The command-line client calls `LookupA` for `example.com` using `1.1.1.1:53`. Local test servers make transport and error scenarios reproducible; the live resolver experiment verifies the path against an external server without making the test suite depend on its availability.

### What I Deliberately Did Not Implement

| Capability | Boundary for this phase |
| --- | --- |
| Recursive resolver | The client asks a configured resolver; it does not follow referrals from root through authoritative servers |
| DNS caching | No retained records, expiry tracking, or cache reuse between lookup calls |
| CNAME following | Raw records can be decoded, but alias chains are not followed |
| Full AAAA lookup API | The type constant exists; the public lookup and extraction path targets IPv4 |
| DNSSEC | No signature or chain-of-trust validation |
| EDNS | No option negotiation, advertised UDP payload-size handling, or extended RCODE interpretation |
| DoH / DoT | No DNS-over-HTTPS or DNS-over-TLS transport |
| Full DNS record-type support | Generic RDATA storage is implemented; type-specific interpretation is limited to A/IN |
| Resolver configuration/discovery | The API takes an explicit server address; the example client uses a fixed resolver rather than discovering OS configuration |

There are also specific limits within the implemented path. Full UDP decoding happens before the TC check, so a truncated packet with an incomplete declared record fails decoding before fallback. Name handling covers ordinary labels and compression pointers with ASCII case comparison; it does not provide a general internationalized-name conversion layer. The client does not perform server failover, automatic UDP retransmission, or TCP connection pooling.

These boundaries keep the experiment focused on explaining how a DNS question becomes bytes, how a response becomes validated records, and how transport behavior affects that process. Each omitted capability can be a separate milestone with its own contract and tests.
