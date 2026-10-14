package rmq

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"

	"github.com/apus-run/gala/components/mq"
	"github.com/apus-run/gala/pkg/lang/safego"
)

var (
	ErrClosed = errors.New("rmq: consumer closed")
	ErrBusy   = errors.New("rmq: lifecycle operation in progress")
)

type Consumer struct {
	lifecycle sync.Mutex
	started   bool
	closeErr  error
	cancel    context.CancelFunc
	active    sync.WaitGroup
	mu        sync.Mutex
	closed    bool
	consumer  rocketmq.PushConsumer
	handler   mq.ConsumerHandler
	topic     string
	selector  consumer.MessageSelector
}

func (c *Consumer) Start() error {
	if !c.lifecycle.TryLock() {
		return ErrBusy
	}
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	closed, handler := c.closed, c.handler
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if c.started {
		return nil
	}
	if handler == nil {
		return errors.New("handler not set")
	}
	lifetime, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	err := c.consumer.Subscribe(c.topic, c.selector, func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return consumer.ConsumeRetryLater, ErrClosed
		}
		c.active.Add(1)
		c.mu.Unlock()
		defer c.active.Done()
		// Preserve SDK context values/deadlines while also honoring Close.
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(lifetime, cancel)
		defer stop()
		defer cancel()
		if lifetime.Err() != nil {
			cancel()
		}
		if err := handleMessages(ctx, handler, msgs); err != nil {
			return consumer.ConsumeRetryLater, err
		}
		if err := lifetime.Err(); err != nil {
			return consumer.ConsumeRetryLater, err
		}
		return consumer.ConsumeSuccess, nil
	})

	if err == nil {
		err = c.consumer.Start()
	}
	if err != nil {
		c.close()
		return err
	}
	c.started = true
	return nil
}

func handleMessages(ctx context.Context, handler mq.ConsumerHandler, msgs []*primitive.MessageExt) (err error) {
	defer safego.Recover(ctx, &err)
	for _, msg := range msgs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if msg == nil {
			return errors.New("rmq: received nil message")
		}
		ext := &mq.MessageExt{
			Message: mq.Message{
				Topic:        msg.Topic,
				Body:         bytes.Clone(msg.Body),
				Tag:          msg.GetTags(),
				PartitionKey: msg.GetShardingKey(),
				Properties:   msg.GetProperties(),
			},
			MsgID: msg.MsgId,
		}
		if err := handler.HandleMessage(ctx, ext); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Close cancels and drains handlers before shutting down the SDK.
// Handlers must honor context cancellation and must not close this consumer.
func (c *Consumer) Close() error {
	if !c.lifecycle.TryLock() {
		return ErrBusy
	}
	defer c.lifecycle.Unlock()
	return c.close()
}

func (c *Consumer) close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.closeErr
	}
	c.closed = true
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.active.Wait()
	// v2.1.2 Shutdown panics before initialization (and can unregister another
	// consumer with the same group). Its late Start failure cleans itself up.
	if c.started {
		c.closeErr = c.consumer.Shutdown()
	}
	return c.closeErr
}

func (c *Consumer) RegisterHandler(h mq.ConsumerHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = h
}
