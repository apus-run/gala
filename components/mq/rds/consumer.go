package rds

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/mq"
)

type Consumer struct {
	client          *redis.Client
	opts            options
	topic, group    string
	timeout         time.Duration
	workers         int
	mu              sync.Mutex
	handler         mq.ConsumerHandler
	started, closed bool
	cancel          context.CancelFunc
	active          sync.WaitGroup
}

var _ mq.Consumer = (*Consumer)(nil)

// RegisterHandler sets the handler for Start; running workers keep its snapshot.
func (c *Consumer) RegisterHandler(h mq.ConsumerHandler) { c.mu.Lock(); c.handler = h; c.mu.Unlock() }
func (c *Consumer) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if c.started {
		return nil
	}
	if c.handler == nil {
		return errors.New("redisstream: handler not set")
	}
	startup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := c.client.XGroupCreateMkStream(startup, StreamKey(c.topic), c.group, "0").Err()
	cancel()
	if err != nil && !redis.HasErrorPrefix(err, "BUSYGROUP") {
		return err
	}
	ctx, stop := context.WithCancel(context.Background())
	c.cancel = stop
	c.started = true
	handler := c.handler
	c.active.Add(c.workers + 1)
	go func() { defer c.active.Done(); c.scheduler(ctx) }()
	for n := 0; n < c.workers; n++ {
		owner := rand.Text()
		go func() { defer c.active.Done(); c.run(ctx, owner, handler) }()
	}
	return nil
}

// Close cancels polling and handlers, then waits for them to return. Handlers
// must honor context and must not close their own consumer. Redis remains owned
// by the application; group state and unacknowledged messages are retained.
func (c *Consumer) Close() error {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
	c.active.Wait()
	return nil
}
func (c *Consumer) pause(ctx context.Context) bool {
	timer := time.NewTimer(c.opts.pollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (c *Consumer) report(ctx context.Context, operation string, err error) {
	if ctx.Err() == nil {
		slog.ErrorContext(ctx, "redisstream operation failed", "operation", operation, "topic", c.topic, "group", c.group, "error", err)
	}
}
func (c *Consumer) scheduler(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.client.Eval(ctx, promote, []string{StreamKey(c.topic), DelayKey(c.topic)}).Err(); err != nil {
			c.report(ctx, "promote", err)
		}
		if !c.pause(ctx) {
			return
		}
	}
}
func (c *Consumer) run(ctx context.Context, owner string, handler mq.ConsumerHandler) {
	cursor := "0-0"
	for ctx.Err() == nil {
		messages, next, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: StreamKey(c.topic), Group: c.group, Consumer: owner, MinIdle: c.opts.claimIdle, Start: cursor, Count: 1}).Result()
		if err != nil {
			c.report(ctx, "claim", err)
			if !c.pause(ctx) {
				return
			}
			continue
		}
		cursor = next
		if len(messages) == 0 {
			streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: c.group, Consumer: owner, Streams: []string{StreamKey(c.topic), ">"}, Count: 1, Block: c.opts.pollInterval}).Result()
			if err != nil {
				if !errors.Is(err, redis.Nil) {
					c.report(ctx, "read", err)
					if !c.pause(ctx) {
						return
					}
				}
				continue
			}
			for _, stream := range streams {
				messages = append(messages, stream.Messages...)
			}
		}
		for _, message := range messages {
			if ctx.Err() != nil {
				return
			}
			if err := c.process(ctx, owner, handler, message); err != nil {
				c.report(ctx, "process", err)
				if !c.pause(ctx) {
					return
				}
			}
		}
	}
}
func (c *Consumer) settle(ctx context.Context, owner string, message redis.XMessage, attempt int64, dead bool, reason string) error {
	action := "ack"
	if dead {
		action = "dead"
	}
	return c.client.Eval(ctx, finish, []string{StreamKey(c.topic), DeadLetterKey(c.topic, c.group)}, c.group, message.ID, owner, attempt, action, reason).Err()
}
func invoke(ctx context.Context, h mq.ConsumerHandler, m *mq.MessageExt) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("redisstream: handler panic: %v", value)
		}
	}()
	err = h.HandleMessage(ctx, m)
	if err == nil {
		err = ctx.Err()
	}
	return err
}
func (c *Consumer) process(ctx context.Context, owner string, handler mq.ConsumerHandler, message redis.XMessage) error {
	pending, err := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: StreamKey(c.topic), Group: c.group, Start: message.ID, End: message.ID, Count: 1}).Result()
	if err != nil {
		return err
	}
	if len(pending) == 0 || pending[0].Consumer != owner {
		return nil
	}
	attempt := pending[0].RetryCount
	if attempt < 1 {
		return errors.New("redisstream: invalid delivery count")
	}
	decoded, err := decode(c.topic, message.Values)
	if err != nil {
		return c.settle(ctx, owner, message, attempt, true, err.Error())
	}
	if attempt-1 > int64(c.opts.maxRetries) {
		return c.settle(ctx, owner, message, attempt, true, "delivery limit exceeded")
	}
	handlerCtx, cancel := context.WithTimeout(ctx, c.timeout)
	err = invoke(handlerCtx, handler, decoded)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return c.settle(ctx, owner, message, attempt, false, "")
	}
	if attempt-1 >= int64(c.opts.maxRetries) {
		return c.settle(ctx, owner, message, attempt, true, err.Error())
	}
	// Leave the entry pending. XAUTOCLAIM retries only this group, preserving its
	// stable message ID and avoiding republishing to other groups.
	return err
}
