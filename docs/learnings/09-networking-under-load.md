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

---

## Day 2 — Throughput and Latency

### Goal

Understand how throughput and request latency behave together as concurrency
increases, especially after available CPU capacity becomes saturated.

I also wanted to test whether the measurements could predict what would happen
when concurrency increased beyond the ten-client experiment.

### Experiment Setup

I kept the Day 1 server, 10,000-hash workload, persistent connections, loopback
networking, and shared 10-second window unchanged. The client continued to allow
only one request in flight per connection.

The client improvement was adding p50 latency. I collected a fresh set of runs
at 1, 2, 4, 6, 8, and 10 clients, then tested a prediction with 20 clients.

### Measurements

Throughput is completed requests divided by the measurement duration. Latency
is the elapsed time from immediately before sending a request until the complete
response arrives. It is client-observed response time, not isolated server
computation time.

The client reported completed requests, errors, unfinished requests, throughput,
and successful-request latency statistics:

- **Average (mean):** the sum of latency samples divided by their count.
- **p50 (median):** the middle sorted sample, or the average of the two middle
  samples when the count is even.
- **p95:** the nearest-rank 95th percentile, describing the slower end of
  successful requests.

The mean and median are different statistics. A small number of slow requests
can pull the mean upward while changing the median much less. Comparing p50,
average, and p95 helps describe behavior that one latency number would hide.

The implementation also reported minimum latency; the unresolved `0s` readings
remain a measurement limitation rather than a result used for interpretation.
CPU readings were manually observed peaks, not averages over the full window.

### Results

Each row is a fresh 10-second run, separate from the Day 1 measurements.

| Clients | Throughput (req/s) | p50 (ms) | Average (ms) | p95 (ms) | Observed CPU peak |
|---:|---:|---:|---:|---:|---:|
| 1 | 1,328.6 | 0.7277 | 0.752554 | 1.0899 | ~30% |
| 2 | 2,094.7 | 0.7427 | 0.954682 | 1.5095 | ~49% |
| 4 | 3,160.2 | 1.22855 | 1.265396 | 1.9259 | ~89% |
| 6 | 3,432.3 | 1.6398 | 1.747818 | 2.8102 | 100% |
| 8 | 3,570.9 | 2.0663 | 2.239841 | 3.9671 | 100% |
| 10 | 3,607.8 | 2.63565 | 2.771283 | 4.8215 | 100% |

All six runs reported zero errors and one unfinished request per client at the
cutoff. CPU readings in this series are retained as reported; process-versus-
whole-machine attribution was not separately confirmed for each reading.

### Observations

From one to four clients, throughput rose from 1,328.6 to 3,160.2 requests/sec.
After around four clients, throughput continued to increase but with diminishing
returns. From four to ten clients, the gain was about 14%, while average latency
rose from 1.265 to 2.771 ms, approximately 2.2 times as high.

p50, average, and p95 increased across the client counts. At ten clients, p50
was 2.636 ms and the average was 2.771 ms, but p95 was 4.822 ms. The typical
request and slower requests therefore experienced different response times.
The p95-minus-p50 gap widened from about 0.362 ms at one client to 2.186 ms
at ten clients.

Similar throughput did not imply similar client experience. Additional
concurrency near saturation increased latency much more than useful throughput.

### Closed-Loop Relationship

This client generates closed-loop load:

```text
send request → wait for response → record latency → send next request
```

Concurrency is configured directly; requests/sec emerges from how quickly
clients complete that cycle. A slower response also delays the next request.

I multiplied measured throughput by average latency, converting milliseconds
to seconds first:

| Clients | Throughput × average latency |
|---:|---:|
| 1 | 0.9998 |
| 2 | 1.9998 |
| 4 | 3.9989 |
| 6 | 5.9980 |
| 8 | 7.9982 |
| 10 | 9.9982 |

The products closely matched configured concurrency. This is consistent with
Little's Law:

```text
L ≈ X × R

L = average number of requests in flight
X = throughput in requests/sec
R = mean request latency in seconds
```

For this continuously active closed-loop workload, with one outstanding request
per client and very little time between requests, `L` is approximately the number
of clients, `N`. That gives the useful approximation `N ≈ X × R`.

This does not mean throughput multiplied by any latency statistic always equals
configured concurrency. The relationship uses the mean, not p50 or p95, and
consistent measurement boundaries. Client think time, failures, and changes in
active client count would affect the interpretation. Finite-window startup and
unfinished requests also keep these measured products from being exact identities.

### Prediction at 20 Clients

The ten-client run measured 3,607.8 requests/sec and 2.771283 ms average latency.
If doubling concurrency did not substantially change throughput, the closed-loop
relationship suggested that average latency would approximately double.

Using a rounded capacity estimate of 3,600 requests/sec:

```text
Expected average latency ≈ N / X
                         ≈ 20 / 3600 seconds
                         ≈ 5.56 ms
```

This was a prediction under an approximately constant-throughput assumption,
not a measurement or a guarantee.

### 20-Client Result

The subsequent run completed 34,532 requests in ten seconds:

| Metric | 10 clients | 20 clients |
|---|---:|---:|
| Completed requests | 36,078 | 34,532 |
| Throughput (req/s) | 3,607.8 | 3,453.2 |
| Average latency (ms) | 2.771283 | 5.788762 |
| p50 latency (ms) | 2.63565 | 5.3968 |
| p95 latency (ms) | 4.8215 | 10.0337 |
| Errors | 0 | 0 |
| Unfinished | 10 | 20 |

Doubling concurrency did not double throughput. Throughput was about 4.3% lower
in this run, while average latency increased approximately 2.09 times and p95
approximately 2.08 times. The result supported the predicted latency increase,
although throughput was not exactly constant.

Using the measured twenty-client throughput and mean latency:

```text
3453.2 requests/sec × 0.005788762 seconds ≈ 19.9898
```

This closely matches the 20 configured clients, consistent with the closed-loop
relationship observed in the earlier runs. No CPU reading was recorded for the
twenty-client run.

### What I Learned

I learned to evaluate throughput alongside typical and tail response times.
A throughput increase can come with a much larger latency cost, and additional
concurrency beyond capacity can increase response time without improving
completed work per second.

The closed-loop client also made the connection between concurrency, throughput,
and average latency concrete. I used that relationship to make a prediction,
then compared it with a new measurement instead of accepting the calculation
as proof.

### Limitations

- Each Day 2 concurrency level has one run. The small throughput decrease at
  twenty clients does not establish a general throughput regression.
- CPU values are observed peaks, not synchronized averages, and their attribution
  was not independently confirmed for this series. No twenty-client CPU value
  was collected.
- The client and server share one machine and use synthetic CPU work over
  loopback. These results do not establish remote-service capacity.
- There is no independently controlled arrival rate or explicit warm-up.
  Closed-loop clients reduce their request rate when responses slow down.
- Only successful responses contribute latency samples. Cutoff requests and
  other measurement limitations from Day 1 still apply.
- The throughput–mean-latency product is consistent with Little's Law under
  this setup. It does not independently locate a queue, measure CPU queue time,
  or establish that every configured client remains active in other experiments.

### Conclusion

Throughput increased substantially as concurrency increased from one to four
clients, then showed diminishing returns while median, average, and p95 latency
continued rising. The twenty-client
test supported the prediction that extra concurrency near capacity would mainly
increase response time. For this closed-loop workload, throughput multiplied by
mean latency closely matched the number of active clients.

---

## Day 3 — Backpressure and Queuing

### Goal

Understand where excess work waits when processing capacity is limited, what
happens when a bounded queue fills, and how blocking, rejection, and client retry
behavior change the response to overload.

### Experiment Setup

I created a separate [queue server](../../load/backpressure/queue/server/main.go)
and [client](../../load/backpressure/queue/client/main.go), preserving the earlier
CPU-bound experiment.

The server listens on `:9200`. Connection handlers submit jobs to a buffered Go
channel with capacity four. One worker processes jobs using an artificial
`time.Sleep(100 * time.Millisecond)` before signaling completion. The handler
then sends `DONE\n`.

This is deliberately different from Days 1–2: the sleep makes queue behavior
visible, rather than creating CPU load. One worker processing one job every
100 ms has a nominal capacity of approximately 10 requests/sec. Scheduling and
I/O overhead can reduce the measured rate.

Clients reuse persistent connections, keep one request in flight per client,
start together, and run for ten seconds. The server reports:

| Field | Meaning |
|---|---|
| `accepted` | Cumulative valid WORK requests read, including requests later rejected |
| `queued` | Cumulative successful channel submissions, not current queue length |
| `completed` | Cumulative jobs whose worker processing finished, not confirmed client responses |
| `rejected` | Cumulative requests refused because queue capacity was unavailable |
| `queue` | Instantaneous buffered jobs divided by channel capacity |
| `submitting` | Handlers currently attempting a blocking channel submission |

Atomic counters allow concurrent updates. Values are read separately, so a log
line is not a transactionally consistent snapshot. Server counters accumulate
across client runs, while client results cover each individual window.

### Blocking-Policy Experiment

The first policy submits directly:

```go
jobs <- job
```

When the channel is full, the connection handler blocks until the worker removes
an item. After submission, the handler waits for its own job to finish before
responding to the client.

| Clients | Completed | Throughput (req/s) | Avg latency (ms) | p50 (ms) | p95 (ms) |
|---:|---:|---:|---:|---:|---:|
| 1 | 89 | 8.9 | 112.104 | 112.195 | 112.986 |
| 2 | 89 | 8.9 | 222.880 | 224.103 | 225.666 |
| 4 | 89 | 8.9 | 441.680 | 448.884 | 451.782 |
| 6 | 88 | 8.8 | 655.013 | 674.045 | 675.749 |
| 8 | 89 | 8.9 | 862.986 | 898.422 | 899.712 |
| 10 | 89 | 8.9 | 1,064.035 | 1,121.230 | 1,124.344 |

Each run recorded zero errors and one unfinished request per client at cutoff.
Throughput stayed near nine requests/sec while average latency rose from about
112 ms to 1.064 seconds. More clients did not make the single worker faster.

The queue was observed full at six clients:

```text
queue=4/4 submitting=1
```

At ten clients, the logs also showed:

```text
queue=4/4 submitting=5
```

These observations distinguish two waiting locations: four jobs buffered in the
channel, and additional connection goroutines blocked outside it. With ten active
clients, one processing job, four queued jobs, and five submitting handlers account
for the outstanding work in that snapshot.

The channel bounds buffered jobs, but does not bound the total number of blocked
producers. Blocking slows each caller's next request, yet connections and waiting
goroutines can still consume resources.

### Rejection and Retry Behavior

I next changed the queue-full policy to a non-blocking channel submission:

```go
select {
case jobs <- job:
    // Wait for processing, then return DONE.
default:
    // Return BUSY without waiting for queue capacity.
}
```

If space is available, processing follows the same path. Otherwise, the server
returns `BUSY\n` on the existing connection. This makes overload explicit and
sheds the rejected attempt before worker processing. The caller decides whether
and when to try again.

This separates server-side admission policy from client retry behavior. The
non-blocking send avoids waiting for queue capacity; response writing and network
scheduling can still take time. Rejection logs showed `submitting=0` even with
`queue=4/4`.

#### Rejection Without Retry Delay

With ten clients, the first rejection experiment immediately retried after
receiving `BUSY`. Three repeated runs produced:

| Run | Completed | Rejected | Throughput (req/s) | Avg successful latency (ms) | Avg rejection latency (µs) |
|---:|---:|---:|---:|---:|---:|
| 1 | 99 | 642,773 | 9.9 | 491.382 | 77.657 |
| 2 | 99 | 634,968 | 9.9 | 491.475 | 78.617 |
| 3 | 99 | 624,321 | 9.9 | 491.710 | 79.960 |

All three runs reported zero errors and ten unfinished requests. Successful
throughput stayed near the worker's nominal limit, but callers generated more
than 600,000 rejected attempts per run. Fast rejection alone did not prevent
excessive retry pressure.

Successful-request latency measures only the attempt that receives DONE. It
excludes earlier rejected attempts, so approximately 491 ms is not the total
client time spent retrying until a successful completion.

#### Rejection With a 50 ms Retry Delay

I then configured the client to wait 50 ms after BUSY before sending its next
request. This is a fixed retry delay, not an exponential retry strategy. The
wait is capped by the remaining experiment duration and lies outside individual
attempt latency measurements.

With ten clients and the same worker, queue, and processing duration:

| Run | Completed | Rejected | Throughput (req/s) | Avg successful latency (ms) | p50 (ms) | p95 (ms) | Avg rejection latency (ms) |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 87 | 755 | 8.7 | 555.488 | 560.808 | 663.121 | 1.310301 |
| 2 | 89 | 785 | 8.9 | 547.444 | 560.179 | 561.477 | 0.024858 |
| 3 | 90 | 780 | 9.0 | 542.687 | 557.888 | 563.737 | 1.088634 |

All runs reported zero errors and five unfinished requests. A client waiting
between attempts at cutoff has no outstanding request to count as unfinished.

Rejected attempts fell from hundreds of thousands to 755–785 per run. Successful
throughput remained near the worker's capacity; the delay changed retry pressure
without creating more processing capacity. Successful latency still excludes
previous BUSY attempts and retry delays.

#### Reproducing the Experiments

Start the server with the selected policy in one terminal, then run a client
command in another:

```sh
# Blocking policy
go run ./load/backpressure/queue/server -policy block
go run ./load/backpressure/queue/client -clients 10

# Stop the blocking server before starting the rejecting server.
go run ./load/backpressure/queue/server -policy reject

# Immediate retry
go run ./load/backpressure/queue/client -clients 10

# Fixed retry delay
go run ./load/backpressure/queue/client -clients 10 -retry-delay 50ms
```

### Comparing the Policies

| Policy | Server behavior when full | Client behavior | Main observed effect |
|---|---|---|---|
| Blocking | Wait for queue capacity | Wait for the response | Work waits inside and outside the queue; latency rises |
| Rejection + immediate retry | Return BUSY | Send another request immediately | No blocked submitters, but very high retry pressure |
| Rejection + fixed delay | Return BUSY | Wait 50 ms, then retry | Far fewer rejected attempts; worker capacity remains unchanged |

A bounded queue provides limited waiting space. It does not solve a sustained
capacity mismatch. Once full, the policy determines whether producers wait or
attempts are rejected. Client behavior determines whether rejection reduces
pressure or causes repeated overload attempts.

### What I Learned

I learned that waiting can exist even without an explicit queue, and a bounded
channel makes only one part of that waiting visible. Blocking a channel sender
moves excess work into waiting connection goroutines; bounding the channel alone
does not bound all outstanding work.

Rejection makes unavailable capacity visible to callers. Immediate retries can
turn that signal into a retry storm, while a fixed delay greatly reduces the
number of attempts. Server protection therefore depends on both admission policy
and how callers react.

Neither a queue nor a retry delay increases worker capacity. They change where
and how callers wait, and whether an attempt is admitted or rejected.

### Limitations

- Artificial sleep creates a controlled service delay, not a realistic CPU,
  database, or downstream-service workload. The nominal 10 requests/sec is not
  an exact measured rate.
- The client is closed-loop. Rejected attempts change its achieved request rate;
  the experiments do not hold an independent arrival rate constant.
- Client and server share one machine. Policy throughput differences can reflect
  scheduling, timing, and retry activity rather than additional worker capacity.
- Successful latency excludes rejected attempts and retry delays. Total time to
  eventual success and per-client fairness were not measured.
- Queue length and submitting counts are instantaneous samples, not complete
  histories. A zero sampled submitting count in rejection mode follows the
  non-blocking admission path, not a claim that all network operations are free
  of blocking.
- Server processing continues for queued jobs after clients reach their cutoff.
  Server completed counts can exceed client completions, and an undrained queue
  could affect a subsequent run.
- Blocking bounds buffered jobs without limiting connections or blocked
  producers. Rejection likewise does not bound connections or all network work.
- This fixed-delay experiment does not implement exponential retry behavior,
  jitter, retry budgets, cancellation of queued jobs, or production overload
  protection. Repeated admission attempts are not guaranteed fair access.

### Conclusion

With one slow worker, more clients primarily increased waiting. A full bounded
queue either blocked producers or returned BUSY, depending on the admission
policy. Adding a 50 ms client retry delay sharply reduced rejected attempts
without increasing processing capacity. Backpressure determines how overload is
communicated and handled; it does not remove the underlying capacity limit.
