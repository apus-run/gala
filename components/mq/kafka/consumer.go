package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/apus-run/gala/pkg/lang/safego"
	"github.com/twmb/franz-go/pkg/kgo"
)

type consumerClient interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	SetOffsets(map[string]map[int32]kgo.EpochOffset)
	AllowRebalance()
	CloseAllowingRebalance()
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
}

// Consumer consumes a topic through a franz-go consumer group, one record at
// a time, committing each record only after its handler succeeds.
// Consumer 通过 franz-go 消费组消费主题：一次处理一条记录，
// 处理器成功后才提交该记录。
type Consumer struct {
	client      consumerClient
	timeout     time.Duration
	mu          sync.Mutex
	handler     mq.ConsumerHandler
	cancel      context.CancelFunc
	done        chan struct{}
	closed      bool
	err         error
	closeClient sync.Once

	maxRetries int
	deadLetter string
	// failing/attempts track the record currently being retried. Records are
	// processed one at a time, so a single slot is enough.
	// failing/attempts 记录当前正在重试的记录。记录逐条处理，单个槽位足够。
	failing  string
	attempts int
}

// RegisterHandler sets the handler for the next Start. The running loop keeps
// its snapshot.
// RegisterHandler 设置下一次 Start 使用的处理器。运行中的循环继续用其快照。
func (c *Consumer) RegisterHandler(handler mq.ConsumerHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

// Start launches the background polling loop | Start 启动后台轮询循环。
func (c *Consumer) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if c.done != nil {
		return nil
	}
	if c.handler == nil {
		return errors.New("kafka: handler not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.done = cancel, make(chan struct{})
	go c.run(ctx, c.handler)
	return nil
}

// Close cancels polling/processing and waits for the handler to return.
// Handlers must honor their context and must not call Close on their own
// consumer.
// Close 取消轮询与处理，并等待处理器返回。处理器必须尊重其 context，
// 且不得在自己的消费者上调用 Close。
func (c *Consumer) Close() error {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
	c.closeClient.Do(c.client.CloseAllowingRebalance)
	return c.Err()
}

// Err reports a terminal fetch error; handler/commit failures are retried.
// Err 返回导致终止的拉取错误；处理器与提交失败会被重试。
func (c *Consumer) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Consumer) run(ctx context.Context, handler mq.ConsumerHandler) {
	defer close(c.done)
	defer c.closeClient.Do(c.client.CloseAllowingRebalance)
	defer c.cancel()
	for ctx.Err() == nil {
		fetches := c.client.PollRecords(ctx, 1)
		if ctx.Err() != nil {
			return
		}
		if err := fetches.Err(); err != nil {
			c.mu.Lock()
			c.err, c.closed = err, true
			c.mu.Unlock()
			slog.ErrorContext(ctx, "kafka consumer stopped", "error", err)
			return
		}
		var failed bool
		for _, record := range fetches.Records() {
			if err := c.process(ctx, handler, record); err != nil {
				if c.skip(ctx, record, err) {
					continue
				}
				// Rewind while rebalance is blocked and no commit is in flight.
				// Never poll past a failed record and later commit over its offset.
				// 在 rebalance 被阻塞且没有进行中的提交时回退位移。
				// 绝不越过失败记录继续拉取，避免后续提交覆盖它的位移。
				c.client.SetOffsets(map[string]map[int32]kgo.EpochOffset{
					record.Topic: {record.Partition: {Epoch: record.LeaderEpoch, Offset: record.Offset}},
				})
				if ctx.Err() == nil {
					slog.ErrorContext(ctx, "kafka message will retry", "message_id", recordID(record), "error", err)
				}
				failed = true
				break
			}
		}
		c.client.AllowRebalance()
		if failed {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// skip reports whether a failed record has used up its retries and was moved
// aside (dead-lettered or dropped) and committed. On false the caller retries
// it.
// skip 判断失败记录是否已耗尽重试并被移开（转死信或丢弃）且已提交。
// 返回 false 时调用方继续重试该记录。
func (c *Consumer) skip(ctx context.Context, record *kgo.Record, cause error) bool {
	// Shutdown is not the record's fault
	// 关闭不是这条记录的问题
	if c.maxRetries <= 0 || ctx.Err() != nil {
		return false
	}
	id := recordID(record)
	if id != c.failing {
		c.failing, c.attempts = id, 0
	}
	if c.attempts++; c.attempts <= c.maxRetries {
		return false
	}
	if c.deadLetter != "" {
		dead := &kgo.Record{Topic: c.deadLetter, Key: record.Key, Value: record.Value,
			Headers: append(append([]kgo.RecordHeader(nil), record.Headers...),
				kgo.RecordHeader{Key: "dlq-source", Value: []byte(id)},
				kgo.RecordHeader{Key: "dlq-error", Value: []byte(cause.Error())})}
		if err := c.client.ProduceSync(ctx, dead).FirstErr(); err != nil {
			slog.ErrorContext(ctx, "kafka dead-letter failed, will retry", "message_id", id, "error", err)
			return false
		}
	}
	if err := c.client.CommitRecords(ctx, record); err != nil {
		return false
	}
	slog.WarnContext(ctx, "kafka message skipped after retries", "message_id", id, "dead_letter", c.deadLetter, "error", cause)
	c.failing = ""
	return true
}

func (c *Consumer) process(ctx context.Context, handler mq.ConsumerHandler, record *kgo.Record) (err error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	defer safego.Recover(ctx, &err)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := handler.HandleMessage(ctx, fromRecord(record)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.client.CommitRecords(ctx, record); err != nil {
		return fmt.Errorf("kafka: commit: %w", err)
	}
	return nil
}
