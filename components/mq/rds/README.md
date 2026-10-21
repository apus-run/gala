# Redis Streams MQ

`rds` implements the existing `mq.Factory`, `Producer` and `Consumer`
interfaces using Redis Streams. Use Redis 7+; tests run against Redis 7.4.
No separate queue abstraction, database or global metrics registration is added.

```go
import (
    "time"

    "github.com/apus-run/gala/components/mq"
    "github.com/apus-run/gala/components/mq/rds"
    "github.com/redis/go-redis/v9"
)

// Inside an application function, with ctx and handler supplied by the caller:
client := redis.NewClient(&redis.Options{
    Addr: "127.0.0.1:6379",
    ContextTimeoutEnabled: true,
    MaxRetries: -1, // avoid transparent replay of an uncertain publish
})
defer client.Close()
factory, err := rds.NewFactory(client,
    rds.WithMaxRetries(3),
    rds.WithPollInterval(time.Second),
    rds.WithClaimIdle(time.Minute),
)
if err != nil { return err }
consumer, err := factory.NewConsumer(mq.ConsumerConfig{
    Topic: "orders", ConsumerGroup: "workers",
    ConsumeGoroutineNums: 4, ConsumeTimeout: 30*time.Second,
})
if err != nil { return err }
defer consumer.Close()
consumer.RegisterHandler(handler) // implements mq.ConsumerHandler
if err := consumer.Start(); err != nil { return err }
producer, err := factory.NewProducer(mq.ProducerConfig{})
if err != nil { return err }
defer producer.Close()
response, err := producer.Send(ctx,
    mq.NewDeferMessage("orders", 5*time.Second, []byte("payload")))
```

Import `github.com/apus-run/gala/components/mq/rds` and
`github.com/redis/go-redis/v9`. Register handlers before Start; running workers
keep that handler snapshot. The factory also works with `mq/registry`.
The application owns Redis: close producers/consumers before closing client.
Start/Close are idempotent; a closed adapter cannot restart. Producer Start is a
lifecycle hook, not a Redis readiness check. After Close, Send, SendBatch and
SendAsync return ErrClosed before checking arguments or caller cancellation;
rejected async calls do not invoke callbacks. Calls already in progress when
Close begins may return cancellation instead. Consumer Start creates its group at
ID 0 so an existing stream's history is visible; only BUSYGROUP is ignored.

## Configuration and message semantics

- Connection/authentication/TLS/database settings belong to the injected client.
  Common config Addr must be empty. Compression, ProducerGroup, producer
  RetryTimes, consumer Orderly and TagExpression return mq.ErrUnsupported.
- Consumer concurrency defaults to 1, handler timeout to 30s. ClaimIdle defaults
  to 1 minute and must exceed the handler timeout. PollInterval defaults to 1s;
  both polling and claim durations must be at least 1ms and are truncated to
  whole milliseconds before checking the claim/handler timeout margin.
- NewProducer requires the injected client's ContextTimeoutEnabled=true and
  returns an error otherwise; ReadTimeout/WriteTimeout must not use -2 (which
  disables socket deadline setting). The adapter does not mutate the borrowed client.
- Producer timeout defaults to 5s and covers the entire send call, including a
  batch and input preparation (CPU work is not forcibly interrupted). Async
  deadlines start at submission and exclude callback execution. ProduceTimeout
  or caller deadline expiry returns context.DeadlineExceeded. A socket timeout
  returned while the operation context is still active preserves the original
  Redis client error (inspect net.Error.Timeout), rather than converting it to
  context.DeadlineExceeded. Client retries can consume the remaining deadline.
  In either case the write may still have reached Redis; do not assume rollback.
  An earlier caller deadline takes precedence. Async calls are bounded to 64
  pending operations/callbacks; overflow
  returns ErrBackpressure and does not invoke the callback.
- Body bytes, Tag and Properties survive delivery. PartitionKey ordering is
  unsupported and returns mq.ErrUnsupported. Delays are millisecond precision,
  rounded up; actual delivery also depends on polling, workload and availability.
- MessageID is an opaque task ID generated per Send, preserved through pending
  retries and delayed promotion. It is also MessageExt.MsgID. It is NOT the native
  Redis entry ID; Offset is 0. Put a business idempotency key in Properties when
  retries across separate Send calls must identify the same operation.
- Inputs are snapshotted before asynchronous dispatch; callers must not mutate
  a message concurrently with the Send/SendAsync call itself.
- SendBatch validates all inputs first, then publishes sequentially. It is not a
  transaction: on failure, earlier messages may have succeeded, and the response
  identifies the last known success. An uncertain current write may also exist.

## Retry, recovery and dead letters

Delivery is **at least once**. A successful handler is ACKed; a failed handler or
panic leaves its original entry pending in that consumer group. XAUTOCLAIM takes
idle pending entries, including those abandoned by crashed processes. Nothing is
republished to other consumer groups during retries.

WithMaxRetries defaults to 3 (initial delivery + 3 retries); 0 sends the first
failure to the DLQ. Redis delivery count is the attempt counter, so crashes and
reclaims count as attempts. A message already beyond the limit goes to the DLQ
without another handler invocation. Malformed envelopes go directly to the DLQ.
Retry readiness is based on ClaimIdle since delivery, not a separate backoff.

ACK and dead-letter settlement verify the current pending owner AND attempt
number in Lua. A stale worker cannot settle a reclaimed attempt. DLQ writes
happen before ACK in the same script; a failed DLQ write leaves the original
pending. Repeating settlement after a lost response does not insert another DLQ
record once the pending entry is acknowledged. This does not make business side
effects exactly once: handlers must be idempotent.

Delayed tasks are unique JSON members in a per-topic ZSet, with due time based
on Redis TIME. Identical payloads remain distinct tasks. Each running consumer
has a scheduler that moves up to 128 due entries per poll using XADD followed by
ZREM inside one Lua script. Multiple schedulers can safely share a topic. When
no consumer runs, delayed tasks remain stored until one starts.

## Lifecycle, retention and observation

Close cancels and waits for accepted work. Handlers must honor context, and
handlers/callbacks must not call Close on their own adapter. Redis I/O deadlines
also bound shutdown latency; cancellation cannot forcibly terminate business
code. A handler returning nil after cancellation/timeout is not treated as
successful. Adapter shutdown leaves the group and its pending messages intact.

StreamKey(topic), DelayKey(topic), and DeadLetterKey(topic, group) expose the
operational keys. Topic/group names are hex-encoded to avoid delimiter clashes.
Stream entries and DLQ records are NOT automatically trimmed or deleted; ACK
only updates the group. Call Consumer.Trim(ctx) periodically or after a full drain: it deletes only
the prefix that every group has both delivered and acknowledged (the floor is
derived from each group's last-delivered ID and lowest pending entry). Boundary
inspection and trimming run atomically in Redis; errors are returned to the caller.
With no groups nothing is trimmed. Groups created later cannot replay trimmed
history. A lost reply can leave the caller uncertain how many entries were removed;
retrying still preserves the current groups' unread and pending messages.
Write-time MAXLEN trimming is deliberately absent — it can drop entries still
pending for a group. Empty consumer metadata may also
be cleaned by operators once it has no pending entries. Retain Redis state and
use appropriate persistence/HA settings; this adapter cannot recover data lost
by eviction, unsafe trimming or Redis failover that loses acknowledged writes.
The current factory accepts a standalone *redis.Client, not a ClusterClient.

The concrete *rds.Consumer exposes Stats(ctx): Pending (delivered but
unacknowledged), Lag (not yet delivered to this group, -1 if unknown), Delayed
(topic-wide) and DeadLetters (group-specific). These independent reads are not a
transactional snapshot. Export them through the application's metrics system;
stream length includes acknowledged history and is not a backlog count.

DLQ entries preserve the original stream fields (`data` is the JSON envelope
with id/body/tag/properties), plus `_source_id`, `_attempts`, `_error`. The body is
base64 in JSON. Inspect using XRange on DeadLetterKey, correct the business issue,
and explicitly republish if desired. No automatic DLQ replay is performed.

## Tests

`rds` is an independent Go module; tests in the parent mq module do not recurse
into it. From the repository root, run `cd components/mq/rds`, then
`go test -race ./...` and `go vet ./...`. It uses miniredis by
default. With an isolated real Redis, run:

```sh
REDISSTREAM_ADDR=127.0.0.1:6379 go test -race -count=3 ./...
```

Tests use random topic keys and delete only those keys after closing adapters.
The real timeout test additionally requires RDS_TEST_PAUSE=1 and uses CLIENT
PAUSE WRITE, affecting the entire server: enable it only on a dedicated disposable
Redis instance. Run it with `RDS_TEST_PAUSE=1 REDISSTREAM_ADDR=127.0.0.1:6379 go test -race -run TestProduceTimeoutRealRedis -count=3 ./...`.
Timeout tests cover both ProduceTimeout/caller deadlines and shorter socket
timeouts, including asynchronous callbacks.
They cover payload copies, batch validation, delayed delivery, concurrent
promotion, failed DLQ writes, stale owners, group-isolated retry budgets, orphan
recovery, cancellation without ACK, async backpressure and shutdown. Concurrent
send/Close tests check repeated Close calls, normal and panicking callbacks,
exactly one callback per accepted async send, slot release and borrowed-client
ownership. Separate scenarios prevent mutual masking: sync_io holds only
synchronous XADD replies across Close; async_callback finishes all publishes
before Close and holds only callbacks. Each scenario runs in a bounded
subprocess, with Redis key cleanup owned by the parent
test so a stuck Close cannot block cleanup. Group lag
is checked on real Redis because miniredis v2.35 reports stream length instead.
These tests do not prove production Redis failover durability or exactly-once
business processing.
