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
}

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
}

// RegisterHandler sets the handler for the next Start. The running loop keeps its snapshot.
func (c *Consumer) RegisterHandler(handler mq.ConsumerHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

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

// Close cancels polling/processing and waits for the handler to return. Handlers
// must honor their context and must not call Close on their own consumer.
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
				// Rewind while rebalance is blocked and no commit is in flight.
				// Never poll past a failed record and later commit over its offset.
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
