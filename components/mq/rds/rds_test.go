package rds

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/apus-run/gala/components/mq"
	"github.com/redis/go-redis/v9"
)

type handlerFunc func(context.Context, *mq.MessageExt) error

func (h handlerFunc) HandleMessage(ctx context.Context, m *mq.MessageExt) error { return h(ctx, m) }

func setup(t *testing.T) (*redis.Client, string) {
	t.Helper()
	addr := os.Getenv("REDISSTREAM_ADDR")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	}
	client := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1, ContextTimeoutEnabled: true, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second})
	t.Cleanup(func() { _ = client.Close() })
	topic := "gala_test_" + rand.Text()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Del(ctx, StreamKey(topic), DelayKey(topic), DeadLetterKey(topic, "workers"), DeadLetterKey(topic, "other")).Err(); err != nil {
			t.Error(err)
		}
	})
	return client, topic
}
func factory(t *testing.T, client *redis.Client, opts ...Option) *Factory {
	t.Helper()
	f, err := NewFactory(client, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func producer(t *testing.T, f *Factory) *Producer {
	t.Helper()
	p, err := f.NewProducer(mq.ProducerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p.(*Producer)
}
func consumer(t *testing.T, f *Factory, topic, group string, timeout time.Duration, h handlerFunc) *Consumer {
	t.Helper()
	c, err := f.NewConsumer(mq.ConsumerConfig{Topic: topic, ConsumerGroup: group, ConsumeTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	c.RegisterHandler(h)
	t.Cleanup(func() { _ = c.Close() })
	return c.(*Consumer)
}
func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func readOne(t *testing.T, client *redis.Client, topic, group, owner string) redis.XMessage {
	t.Helper()
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, StreamKey(topic), group, "0").Err(); err != nil && !redis.HasErrorPrefix(err, "BUSYGROUP") {
		t.Fatal(err)
	}
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: owner, Streams: []string{StreamKey(topic), ">"}, Count: 1, Block: -1}).Result()
	if err != nil || len(streams) != 1 || len(streams[0].Messages) != 1 {
		t.Fatalf("read: %v %v", streams, err)
	}
	return streams[0].Messages[0]
}

func TestPublishMetadataAndValidateWholeBatch(t *testing.T) {
	client, topic := setup(t)
	p := producer(t, factory(t, client))
	ctx := context.Background()
	message := mq.NewMessage(topic, []byte{0, 1, 255}).WithTag("created").WithProperties(map[string]string{"trace": "42"})
	response, err := p.Send(ctx, message)
	if err != nil {
		t.Fatal(err)
	}
	message.Body[0] = 99
	message.Properties["trace"] = "changed"
	entries, err := client.XRange(ctx, StreamKey(topic), "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	got, err := decode(topic, entries[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	if got.MsgID != response.MessageID || got.Body[0] != 0 || got.Tag != "created" || got.Properties["trace"] != "42" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if _, err := p.SendBatch(ctx, []*mq.Message{mq.NewMessage(topic, nil), nil}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if client.XLen(ctx, StreamKey(topic)).Val() != 1 {
		t.Fatal("validation published a partial batch")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Send(ctx, message); !errors.Is(err, ErrClosed) {
		t.Fatalf("send after close: %v", err)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal("producer closed borrowed client")
	}
}

func TestDelayPromotionIsAtomicAndKeepsIdenticalPayloads(t *testing.T) {
	client, topic := setup(t)
	p := producer(t, factory(t, client))
	ctx := context.Background()
	a, err := p.Send(ctx, mq.NewDeferMessage(topic, time.Hour, []byte("same")))
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Send(ctx, mq.NewDeferMessage(topic, time.Hour, []byte("same")))
	if err != nil {
		t.Fatal(err)
	}
	if a.MessageID == b.MessageID || client.ZCard(ctx, DelayKey(topic)).Val() != 2 {
		t.Fatal("identical tasks collapsed")
	}
	if err := client.Eval(ctx, promote, []string{StreamKey(topic), DelayKey(topic)}).Err(); err != nil {
		t.Fatal(err)
	}
	if client.XLen(ctx, StreamKey(topic)).Val() != 0 {
		t.Fatal("task delivered early")
	}
	entries, err := client.ZRange(ctx, DelayKey(topic), 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := client.ZAdd(ctx, DelayKey(topic), redis.Z{Score: 0, Member: entry}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Set(ctx, StreamKey(topic), "wrong type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Eval(ctx, promote, []string{StreamKey(topic), DelayKey(topic)}).Err(); err == nil {
		t.Fatal("wrong destination accepted")
	}
	if client.ZCard(ctx, DelayKey(topic)).Val() != 2 {
		t.Fatal("failed promotion lost delayed tasks")
	}
	if err := client.Del(ctx, StreamKey(topic)).Err(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := client.Eval(ctx, promote, []string{StreamKey(topic), DelayKey(topic)}).Err(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if client.ZCard(ctx, DelayKey(topic)).Val() != 0 || client.XLen(ctx, StreamKey(topic)).Val() != 2 {
		t.Fatal("concurrent promotion lost or duplicated tasks")
	}
	rows, err := client.XRange(ctx, StreamKey(topic), "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		message, err := decode(topic, row.Values)
		if err != nil {
			t.Fatal(err)
		}
		seen[message.MsgID] = true
	}
	if !seen[a.MessageID] || !seen[b.MessageID] {
		t.Fatal("promotion changed message identity")
	}
}

func TestDeadLetterFailureRetainsPendingAndStaleOwnersCannotSettle(t *testing.T) {
	client, topic := setup(t)
	f := factory(t, client, WithMaxRetries(0))
	p := producer(t, f)
	ctx := context.Background()
	if _, err := p.Send(ctx, mq.NewMessage(topic, []byte("work"))); err != nil {
		t.Fatal(err)
	}
	original := readOne(t, client, topic, "workers", "old")
	c := consumer(t, f, topic, "workers", time.Second, func(context.Context, *mq.MessageExt) error { return errors.New("failed") })
	if err := client.Set(ctx, DeadLetterKey(topic, "workers"), "wrong type", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.process(ctx, "old", c.handler, original); err == nil {
		t.Fatal("DLQ failure ignored")
	}
	if client.XPending(ctx, StreamKey(topic), "workers").Val().Count != 1 {
		t.Fatal("failed DLQ write ACKed original")
	}
	if err := client.Del(ctx, DeadLetterKey(topic, "workers")).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.XClaim(ctx, &redis.XClaimArgs{Stream: StreamKey(topic), Group: "workers", Consumer: "new", MinIdle: 0, Messages: []string{original.ID}}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.settle(ctx, "old", original, 1, true, "stale"); err != nil {
		t.Fatal(err)
	}
	if err := c.settle(ctx, "new", original, 1, true, "old attempt"); err != nil {
		t.Fatal(err)
	}
	if client.XLen(ctx, DeadLetterKey(topic, "workers")).Val() != 0 || client.XPending(ctx, StreamKey(topic), "workers").Val().Count != 1 {
		t.Fatal("stale attempt settled current owner")
	}
	for range 2 {
		if err := c.settle(ctx, "new", original, 2, true, "failed"); err != nil {
			t.Fatal(err)
		}
	}
	if client.XPending(ctx, StreamKey(topic), "workers").Val().Count != 0 || client.XLen(ctx, DeadLetterKey(topic, "workers")).Val() != 1 {
		t.Fatal("DLQ settlement was not idempotent")
	}
	rows, err := client.XRange(ctx, DeadLetterKey(topic, "workers"), "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Values["data"] != original.Values["data"] || rows[0].Values["_source_id"] != original.ID {
		t.Fatal("DLQ lost original content")
	}
}

func TestRetryBudgetAndConsumerGroupIsolation(t *testing.T) {
	client, topic := setup(t)
	f := factory(t, client, WithMaxRetries(2), WithPollInterval(10*time.Millisecond), WithClaimIdle(50*time.Millisecond))
	p := producer(t, f)
	var failures, successes atomic.Int64
	ids := make(chan string, 8)
	bad := consumer(t, f, topic, "workers", 20*time.Millisecond, func(_ context.Context, m *mq.MessageExt) error { ids <- m.MsgID; failures.Add(1); panic("retry me") })
	good := consumer(t, f, topic, "other", 20*time.Millisecond, func(context.Context, *mq.MessageExt) error { successes.Add(1); return nil })
	if err := bad.Start(); err != nil {
		t.Fatal(err)
	}
	if err := good.Start(); err != nil {
		t.Fatal(err)
	}
	response, err := p.Send(context.Background(), mq.NewMessage(topic, []byte("work")))
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return client.XLen(context.Background(), DeadLetterKey(topic, "workers")).Val() == 1 && successes.Load() == 1
	})
	if failures.Load() != 3 {
		t.Fatalf("want initial + 2 retries, got %d", failures.Load())
	}
	for range 3 {
		if id := <-ids; id != response.MessageID {
			t.Fatalf("retry changed identity: %s", id)
		}
	}
	if client.XLen(context.Background(), StreamKey(topic)).Val() != 1 {
		t.Fatal("retry republished to other groups")
	}
}

func TestAbandonedPendingDeliveryIsRecovered(t *testing.T) {
	client, topic := setup(t)
	f := factory(t, client, WithPollInterval(10*time.Millisecond), WithClaimIdle(50*time.Millisecond))
	p := producer(t, f)
	ctx := context.Background()
	response, err := p.Send(ctx, mq.NewMessage(topic, []byte("orphan")))
	if err != nil {
		t.Fatal(err)
	}
	original := readOne(t, client, topic, "workers", "dead-process")
	if err := client.Do(ctx, "XCLAIM", StreamKey(topic), "workers", "dead-process", 0, original.ID, "IDLE", 1000, "JUSTID").Err(); err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 8)
	c := consumer(t, f, topic, "workers", 20*time.Millisecond, func(_ context.Context, m *mq.MessageExt) error { received <- m.MsgID; return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-received:
		if id != response.MessageID {
			t.Fatal("recovery changed ID")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("orphan never recovered")
	}
	eventually(t, func() bool { return client.XPending(ctx, StreamKey(topic), "workers").Val().Count == 0 })
}

func TestCloseCancelsHandlerWithoutAcknowledging(t *testing.T) {
	client, topic := setup(t)
	f := factory(t, client, WithPollInterval(10*time.Millisecond), WithClaimIdle(2*time.Second))
	p := producer(t, f)
	entered := make(chan struct{}, 1)
	c := consumer(t, f, topic, "workers", time.Second, func(ctx context.Context, _ *mq.MessageExt) error { entered <- struct{}{}; <-ctx.Done(); return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Send(context.Background(), mq.NewMessage(topic, nil)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("restart after close: %v", err)
	}
	if client.XPending(context.Background(), StreamKey(topic), "workers").Val().Count != 1 {
		t.Fatal("shutdown acknowledged canceled work")
	}
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal("consumer closed borrowed Redis client")
	}
}

func TestAsyncBackpressureAndCloseWaitForCallbacks(t *testing.T) {
	client, topic := setup(t)
	p := producer(t, factory(t, client))
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int64
	callback := func(context.Context, mq.SendResponse, error) { <-release; calls.Add(1) }
	for range 64 {
		if err := p.SendAsync(context.Background(), callback, mq.NewMessage(topic, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.SendAsync(context.Background(), callback, mq.NewMessage(topic, nil)); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("missing backpressure: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = p.Close(); close(done) }()
	select {
	case <-done:
		t.Fatal("Close returned before callbacks")
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close stuck")
	}
	if calls.Load() != 64 {
		t.Fatalf("callback count=%d", calls.Load())
	}
}

func TestConfigurationValidation(t *testing.T) {
	client, topic := setup(t)
	if _, err := NewFactory(nil); err == nil {
		t.Fatal("nil client accepted")
	}
	for _, option := range []Option{WithMaxRetries(-1), WithPollInterval(0), WithClaimIdle(0)} {
		if _, err := NewFactory(client, option); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
	f := factory(t, client)
	precise := factory(t, client, WithClaimIdle(10*time.Millisecond+time.Nanosecond))
	if _, err := precise.NewConsumer(mq.ConsumerConfig{Topic: topic, ConsumerGroup: "workers", ConsumeTimeout: 10 * time.Millisecond}); err == nil {
		t.Fatal("millisecond rounding removed the claim/handler timeout margin")
	}
	for _, cfg := range []mq.ConsumerConfig{{Topic: topic}, {Topic: topic, ConsumerGroup: "workers", Orderly: true}, {Topic: topic, ConsumerGroup: "workers", ConsumeTimeout: time.Minute}} {
		if _, err := f.NewConsumer(cfg); err == nil {
			t.Fatalf("accepted config: %+v", cfg)
		}
	}
	c, err := f.NewConsumer(mq.ConsumerConfig{Topic: topic, ConsumerGroup: "workers"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err == nil {
		t.Fatal("started without handler")
	}
	_ = c.Close()
	for _, cfg := range []mq.ProducerConfig{{Addr: []string{"localhost:6379"}}, {RetryTimes: 1}, {Compression: mq.CompressionZSTD}} {
		if _, err := f.NewProducer(cfg); !errors.Is(err, mq.ErrUnsupported) {
			t.Fatalf("unsupported config: %v", err)
		}
	}
	p := producer(t, f)
	if err := p.SendAsync(context.Background(), nil, mq.NewMessage(topic, nil)); err == nil {
		t.Fatal("nil callback accepted")
	}
	if _, err := p.Send(context.Background(), mq.NewOrderlyMessage(topic, "key", nil)); !errors.Is(err, mq.ErrUnsupported) {
		t.Fatalf("order accepted: %v", err)
	}
}

func TestDelayedMessageDeliveryAndStats(t *testing.T) {
	client, topic := setup(t)
	f := factory(t, client, WithPollInterval(10*time.Millisecond), WithClaimIdle(time.Second))
	p := producer(t, f)
	received := make(chan *mq.MessageExt, 1)
	c := consumer(t, f, topic, "workers", 100*time.Millisecond, func(_ context.Context, m *mq.MessageExt) error { received <- m; return nil })
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	response, err := p.Send(context.Background(), mq.NewDeferMessage(topic, 80*time.Millisecond, []byte("later")))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-received:
		if m.MsgID != response.MessageID || string(m.Body) != "later" {
			t.Fatal("delayed payload/identity changed")
		}
		if time.Since(start) < 78*time.Millisecond {
			t.Fatal("delayed task delivered too early")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delayed task was never delivered")
	}
	eventually(t, func() bool {
		stats, err := c.Stats(context.Background())
		return err == nil && stats.Pending == 0 && stats.Delayed == 0 && stats.DeadLetters == 0
	})
	// miniredis v2.35 reports total stream length as lag. Verify real group lag
	// against Redis, where acknowledged retained entries must not count as backlog.
	if os.Getenv("REDISSTREAM_ADDR") != "" {
		stats, err := c.Stats(context.Background())
		if err != nil || stats.Lag != 0 {
			t.Fatalf("acknowledged history counted as lag: %+v %v", stats, err)
		}
	}
	if client.XLen(context.Background(), StreamKey(topic)).Val() != 1 {
		t.Fatal("ACK must retain history for other groups")
	}
}

func TestMalformedEnvelopeGoesToDeadLetterWithoutCallingHandler(t *testing.T) {
	client, topic := setup(t)
	ctx := context.Background()
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: StreamKey(topic), Values: map[string]any{"data": "not JSON"}}).Err(); err != nil {
		t.Fatal(err)
	}
	raw := readOne(t, client, topic, "workers", "owner")
	f := factory(t, client)
	c := consumer(t, f, topic, "workers", time.Second, func(context.Context, *mq.MessageExt) error { t.Error("malformed message reached handler"); return nil })
	if err := c.process(ctx, "owner", c.handler, raw); err != nil {
		t.Fatal(err)
	}
	if client.XPending(ctx, StreamKey(topic), "workers").Val().Count != 0 || client.XLen(ctx, DeadLetterKey(topic, "workers")).Val() != 1 {
		t.Fatal("malformed message was lost or kept pending")
	}
}

func TestTrimRemovesOnlyFullyAcknowledgedPrefix(t *testing.T) {
	client, topicBase := setup(t)
	ctx := context.Background()

	factory, err := NewFactory(client)
	if err != nil {
		t.Fatal(err)
	}
	group := func(topic, name string) {
		t.Cleanup(func() {
			if err := client.Del(ctx, StreamKey(topic)).Err(); err != nil {
				t.Error(err)
			}
		})
		if err := client.XGroupCreateMkStream(ctx, StreamKey(topic), name, "0").Err(); err != nil && !redis.HasErrorPrefix(err, "BUSYGROUP") {
			t.Fatal(err)
		}
	}
	consumer := func(topic, name string) *Consumer {
		c, err := factory.NewConsumer(mq.ConsumerConfig{Topic: topic, ConsumerGroup: name})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c.(*Consumer)
	}
	// deliver reads every new entry for the group and acknowledges the first ack of them.
	deliver := func(topic, name string, ack int) {
		res, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: name, Consumer: name + "-worker", Count: 10, Streams: []string{StreamKey(topic), ">"},
		}).Result()
		if err != nil {
			t.Fatal(err)
		}
		for i, msg := range res[0].Messages {
			if i < ack {
				if err := client.XAck(ctx, StreamKey(topic), name, msg.ID).Err(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	seed := func(topic string) {
		for range 3 {
			if err := client.XAdd(ctx, &redis.XAddArgs{Stream: StreamKey(topic), Values: map[string]any{"data": `{"id":"x"}`}}).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("all acknowledged trims everything", func(t *testing.T) {
		topic := topicBase + "-trim-all"
		group(topic, "a")
		group(topic, "b")
		seed(topic)
		deliver(topic, "a", 3)
		deliver(topic, "b", 3)
		removed, err := consumer(topic, "a").Trim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if removed != 3 {
			t.Fatalf("want 3 removed, got %d", removed)
		}
		if n := client.XLen(ctx, StreamKey(topic)).Val(); n != 0 {
			t.Fatalf("stream should be empty, got %d", n)
		}
	})

	t.Run("pending entry stops the trim", func(t *testing.T) {
		topic := topicBase + "-trim-pending"
		group(topic, "a")
		group(topic, "b")
		seed(topic)
		deliver(topic, "a", 3)
		deliver(topic, "b", 2) // the third stays pending for group b
		removed, err := consumer(topic, "a").Trim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if removed != 2 {
			t.Fatalf("want 2 removed before the pending entry, got %d", removed)
		}
		if n := client.XLen(ctx, StreamKey(topic)).Val(); n != 1 {
			t.Fatalf("pending entry must survive, got %d left", n)
		}
	})

	t.Run("lagging group keeps everything", func(t *testing.T) {
		topic := topicBase + "-trim-lag"
		group(topic, "a")
		group(topic, "b")
		seed(topic)
		deliver(topic, "a", 3) // group b never read
		removed, err := consumer(topic, "a").Trim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if removed != 0 {
			t.Fatalf("nothing is safe while a group lags, removed %d", removed)
		}
	})
}
