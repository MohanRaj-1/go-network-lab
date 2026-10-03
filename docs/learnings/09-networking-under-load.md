# Networking Under Load

## Introduction

This phase investigates how a Go network service behaves as concurrent work
increases. The aim is to connect measured behavior to resource use and understand:

- concurrency under load;
- throughput and latency;
- backpressure and queuing;
- resource limits and server protection;
- behavior near resource saturation.

The experiments use small, controlled workloads. Each experiment adds knowledge
to this document without requiring a general-purpose load-testing framework.

---

## Day 1 — Concurrency Under Load

### Goal

Measure how increasing client concurrency changes throughput, request latency,
CPU utilization, and server behavior as available CPU capacity becomes occupied.

### Hypothesis

If CPU capacity is the limiting resource, increasing concurrency should improve
throughput while capacity is available. Once that capacity becomes saturated,
throughput should approach a ceiling while contention and latency increase.

### Experiment Setup

I implemented a [TCP server](../../load/concurrency/server/main.go) listening on
`:9100` and a [load client](../../load/concurrency/client/main.go). The server
handles each connection in its own goroutine.

Each client:

1. establishes one TCP connection;
2. signals that connection setup is finished;
3. waits for the common start signal;
4. sends `WORK\n` and waits for a complete response;
5. records latency and repeats until the shared deadline.

Connections remain open between requests. Each client has only one request in
flight, so N connected clients can have at most N outstanding requests.
Connection establishment is outside the measurement window.

A readiness wait group accounts for every connection attempt, including failures.
The coordinator sets an absolute deadline and closes a channel to release the
clients. A second wait group tracks completion. Each client owns its measurements
and sends one final result through a buffered channel for aggregation.

The client and server ran on the same Windows machine over loopback. Concurrency
was changed through the client's `const n`.

### Workload

Every request starts with the same 32-byte value, whose first byte is 1, and
performs 10,000 successive SHA-256 computations. Each hash becomes the next
input. The server replies with `DONE <checksum>\n`, containing the first four
bytes of the final hash in hexadecimal.

The computation and iteration count remain fixed between runs. There is no
artificial sleep, database access, disk access, or per-request logging. CPU
capacity is the primary resource under investigation.

### Measurements

Each run lasts 10 seconds. Measurements include:

- completed requests;
- throughput, calculated as completed requests divided by 10 seconds;
- errors and requests unfinished at the shared cutoff;
- average and p95 successful-request latency;
- server-process and whole-machine CPU observations from Task Manager.

Latency is measured immediately before sending a request until the complete
response is read. It includes client/server scheduling effects, CPU contention
and computation, and loopback network I/O. These components are not measured
separately. The client computes p95 using the nearest-rank method on a sorted copy of
successful latency samples. Requests interrupted by the shared deadline are
counted as unfinished rather than as errors.

### Initial Results

The smaller-concurrency series used one run per level. CPU readings are manual
observations, mostly reported as peaks, rather than sampled averages.

| Clients | Completed | Throughput (req/s) | Errors | Unfinished | Server CPU |    Whole CPU | Avg latency (ms) | p95 latency (ms) |
| ------: | --------: | -----------------: | -----: | ---------: | ---------: | -----------: | ---------------: | ---------------: |
|       1 |    12,731 |            1,273.1 |      0 |          1 |        28% |       30–35% |            0.785 |            1.138 |
|       2 |    19,222 |            1,922.2 |      0 |          2 |  Up to 39% |       45–50% |            1.040 |            1.569 |
|       4 |    31,876 |            3,187.6 |      0 |          4 |  Up to 79% |       85–90% |            1.254 |            1.905 |
|       6 |    34,430 |            3,443.0 |      0 |          6 |  Up to 93% |      99–100% |            1.742 |            2.832 |
|       8 |    35,237 |            3,523.7 |      0 |          8 |  Up to 95% |      99–100% |            2.270 |            4.013 |
|      10 |    35,876 |            3,587.6 |      0 |         10 |  Up to 94% |      99–100% |            2.786 |            5.019 |

### Observations

From one to four clients, throughput increased from about 1,273 to 3,188
requests/sec while server CPU usage rose from 28% to an observed peak of 79%.
Adding clients helped keep more CPU capacity occupied.

From six to ten clients, throughput rose only from 3,443 to 3,588 requests/sec,
while average latency increased from 1.742 to 2.786 ms. p95 increased from
2.832 to 5.019 ms. At these levels, extra concurrency produced much more growth
in waiting time than in completed work per second.

No errors were recorded in the smaller-concurrency series. Each run ended with
one unfinished request per client, consistent with clients still waiting for
responses at the deliberate cutoff.

### CPU Saturation

At four clients, whole-machine CPU was observed at 85–90%. By six clients it
spiked to 99–100%, with the server process reaching approximately 93%.

These readings, together with the flattening throughput curve, provide strong
evidence that CPU capacity was limiting this experiment. The observed transition
was around four to six clients for this machine and workload. It is not an exact
or universal client-count threshold.

The system was already operating in the saturated region at ten clients. Higher
client counts added runnable work without adding CPU capacity. The load client
and other processes also used CPU, so the machine's capacity was shared.

### Throughput and Latency

Throughput measures completed work per second; latency measures how long an
individual request waits for completion. These can move differently.

From four to ten clients, throughput increased by about 13%, while average
latency more than doubled. The server continued completing work at a similar
rate, but more clients were competing for that rate.

p95 describes the slower end of successful requests. At ten clients, average
latency was approximately 2.786 ms, while p95 was 5.019 ms. Reporting only
throughput or average latency would miss part of the client experience.

### Higher Concurrency

The 10, 25, 50, 100, 250, and 500-client experiments were repeated three times
per level. Mean throughput across runs remained roughly between 3.2 and 3.5k
requests/sec. Mean average latency rose from about 3 ms at ten clients to
30 ms at 100 and approximately 73–74 ms at 250–500.

Separate CPU observations across these levels showed whole-machine utilization
reaching approximately 99–100%, with server-process usage reported around 92%
on average. These were manual observations rather than instrumented averages.

Errors appeared at 250 and 500 clients. Their causes were not recorded separately,
and each worker exits after its first failure. Requested concurrency therefore
does not prove that all clients connected or remained active throughout the run.
The similar latency at 250 and 500 clients cannot establish equivalent service
behavior, especially when failed requests are excluded from latency statistics.

### What I Learned

I learned that concurrency improves throughput when it helps use spare capacity.
Once the limiting resource is saturated, additional concurrency can primarily
increase contention and latency.

Additional concurrency is not always harmful: while the limiting resource still
has available capacity, it can improve throughput, as the one-to-four-client
results demonstrated.

Goroutines allow concurrent progress, but do not create CPU capacity. The number
of clients or runnable goroutines is different from the number executing CPU
work at the same instant. `GOMAXPROCS`, available logical CPUs, and competing
processes constrain parallel execution.

I also learned to compare throughput with latency and resource utilization.
A similar throughput number can describe very different waiting times for
clients. Repeated runs and explicit error counts help keep that comparison
honest.

### Limitations

- The work is synthetic and CPU-bound. These results do not represent every
  network service or workload.
- The workload is closed-loop: clients wait for responses before issuing the
  next request. Concurrency is controlled directly; arrival rate is not
  independently controlled.
- Database contention, disk I/O, bandwidth limits, and downstream-service
  latency were not investigated.
- Client and server share one machine. This is not a distributed load test,
  and loopback timing does not represent a remote network.
- CPU readings were approximate manual observations. Peaks are not full-window
  averages, and the smaller-concurrency series has only one run per level.
- Hardware details, Go version, and actual `GOMAXPROCS` were not recorded with
  the measurements. There was no explicit warm-up.
- The latency implementation also reported a `0s` minimum in these runs.
  This requires further investigation into timing and measurement behavior;
  it does not establish that computation took zero time.
- Errors combine startup and workload failures. Successful connection count
  is not reported, and the experiment proceeds after setup failures.
- Latency statistics exclude failed and unfinished requests.
- The client validates the response structure and checksum encoding, but does
  not independently verify that the returned checksum matches the expected
  SHA-256 result.

### Conclusion

For this workload, adding clients first improved CPU utilization and throughput.
The machine approached saturation around four to six clients. Beyond that
region, throughput approached a ceiling while average and p95 latency continued
rising. The experiment gave me evidence for the relationship between concurrency,
resource capacity, and waiting time.
