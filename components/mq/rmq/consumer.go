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
	// ErrClosed is returned by operations on a closed consumer
	// | ErrClosed 由已关闭消费者上的操作返回。
	ErrClosed = errors.New("rmq: consumer closed")
	// ErrBusy is returned when another lifecycle call is still in progress
	// | ErrBusy 表示另一个生命周期调用正在进行。
	ErrBusy = errors.New("rmq: lifecycle operation in progress")
)

// Consumer consumes one topic as a RocketMQ push consumer; a handler error
// asks RocketMQ to redeliver later.
// Consumer 以 RocketMQ push 消费者的方式消费一个主题；处理器出错时请求
// RocketMQ 稍后重投。
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

// Start subscribes to the topic and starts the push consumer | Start 订阅主题并启动 push 消费者。
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
		// 保留 SDK context 的值与截止时间，同时让 Close 也能生效。
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
// Close 先取消并排空处理器，再关闭 SDK。处理器必须尊重 context 取消，
// 且不得关闭自己的消费者。
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
	// v2.1.2 的 Shutdown 在初始化之前会 panic（且可能反注册同组的另一个
	// 消费者）。Start 晚期失败会自行清理。
	if c.started {
		c.closeErr = c.consumer.Shutdown()
	}
	return c.closeErr
}

// RegisterHandler sets the handler for the next Start | RegisterHandler 设置下一次 Start 使用的处理器。
func (c *Consumer) RegisterHandler(h mq.ConsumerHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = h
}
