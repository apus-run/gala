package nsq

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/apus-run/gala/pkg/lang/safego"
	nsqclient "github.com/nsqio/go-nsq"
)

type consumerClient interface {
	ConnectToNSQDs([]string) error
	Stop()
}

type Consumer struct {
	client    consumerClient
	stop      <-chan int
	cancel    context.CancelFunc
	addrs     []string
	topic     string
	timeout   time.Duration
	lifecycle sync.Mutex
	started   bool
	mu        sync.Mutex
	closed    bool
	handler   mq.ConsumerHandler
	active    sync.WaitGroup
}

func (c *Consumer) RegisterHandler(handler mq.ConsumerHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
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
		return errors.New("nsq: handler not set")
	}
	if err := c.client.ConnectToNSQDs(c.addrs); err != nil {
		c.close()
		return err
	}
	c.started = true
	return nil
}

func (c *Consumer) Close() error {
	if !c.lifecycle.TryLock() {
		return ErrBusy
	}
	defer c.lifecycle.Unlock()
	c.close()
	return nil
}

func (c *Consumer) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.client.Stop()
	<-c.stop
	// SDK StopChan can close after its shutdown timeout even with a live handler.
	c.active.Wait()
}

func (c *Consumer) handle(ctx context.Context, message *nsqclient.Message) (err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	handler := c.handler
	c.active.Add(1)
	c.mu.Unlock()
	defer c.active.Done()
	defer safego.Recover(ctx, &err)
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if handler == nil {
		return errors.New("nsq: handler not set")
	}
	if message == nil {
		return errors.New("nsq: nil message")
	}
	err = handler.HandleMessage(ctx, &mq.MessageExt{
		Message: mq.Message{Topic: c.topic, Body: bytes.Clone(message.Body)},
		MsgID:   string(message.ID[:]),
	})
	if err != nil {
		return err
	}
	// A nil handler return after shutdown/timeout must still trigger REQ, not FIN.
	return ctx.Err()
}
