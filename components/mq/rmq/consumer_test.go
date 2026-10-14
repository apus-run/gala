package rmq

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/rocketmq-client-go/v2"
	rconsumer "github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apus-run/gala/components/mq"
)

type testPushConsumer struct {
	rocketmq.PushConsumer
	callback               func(context.Context, ...*primitive.MessageExt) (rconsumer.ConsumeResult, error)
	startErr, subscribeErr error
	started                bool
	starts, stops          int
	shutdownErr            error
}

func (c *testPushConsumer) Subscribe(_ string, _ rconsumer.MessageSelector, cb func(context.Context, ...*primitive.MessageExt) (rconsumer.ConsumeResult, error)) error {
	c.callback = cb
	return c.subscribeErr
}
func (c *testPushConsumer) Start() error    { c.started = true; c.starts++; return c.startErr }
func (c *testPushConsumer) Shutdown() error { c.stops++; return c.shutdownErr }

type handlerFunc func(context.Context, *mq.MessageExt) error

func (f handlerFunc) HandleMessage(ctx context.Context, msg *mq.MessageExt) error { return f(ctx, msg) }

func TestConsumerAcknowledgement(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			sdk := &testPushConsumer{}
			c := &Consumer{consumer: sdk, topic: "events"}
			defer c.Close()
			calls := 0
			c.RegisterHandler(handlerFunc(func(_ context.Context, m *mq.MessageExt) error {
				calls++
				if m.MsgID != "id" || m.Tag != "tag" || m.PartitionKey != "key" {
					t.Fatalf("metadata=%+v", m)
				}
				if mode == "error" {
					return errors.New("failure")
				}
				if mode == "panic" {
					panic("failure")
				}
				return nil
			}))
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			msg := &primitive.MessageExt{Message: *primitive.NewMessage("events", nil).WithTag("tag").WithShardingKey("key"), MsgId: "id"}
			result, err := sdk.callback(ctx, msg, msg)
			if mode == "success" {
				if result != rconsumer.ConsumeSuccess || err != nil || calls != 2 {
					t.Fatalf("result=%v err=%v calls=%d", result, err, calls)
				}
			} else {
				if result != rconsumer.ConsumeRetryLater || err == nil || calls > 1 {
					t.Fatalf("result=%v err=%v calls=%d", result, err, calls)
				}
			}
		})
	}
}

func TestConsumerSubscribeFailure(t *testing.T) {
	failure := errors.New("subscription failed")
	sdk := &testPushConsumer{subscribeErr: failure}
	c := &Consumer{consumer: sdk}
	defer c.Close()
	if err := c.Start(); err == nil {
		t.Fatal("missing handler accepted")
	}
	c.RegisterHandler(handlerFunc(func(context.Context, *mq.MessageExt) error { return nil }))
	if err := c.Start(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if sdk.started {
		t.Fatal("started after failed subscription")
	}
}

func TestConsumerRetryPreservesBody(t *testing.T) {
	sdk := &testPushConsumer{}
	c := &Consumer{consumer: sdk, topic: "events"}
	defer c.Close()
	c.RegisterHandler(handlerFunc(func(_ context.Context, msg *mq.MessageExt) error {
		if string(msg.Body) != "original" {
			t.Fatalf("retry body changed: %s", msg.Body)
		}
		msg.Body[0] = 'X'
		return errors.New("retry")
	}))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	msg := &primitive.MessageExt{Message: *primitive.NewMessage("events", []byte("original"))}
	for range 2 {
		if result, err := sdk.callback(context.Background(), msg); result != rconsumer.ConsumeRetryLater || err == nil {
			t.Fatalf("result=%v error=%v", result, err)
		}
	}
}
