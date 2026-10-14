package nsq

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apus-run/gala/components/mq"
	nsqclient "github.com/nsqio/go-nsq"
)

type handlerFunc func(context.Context, *mq.MessageExt) error

func (f handlerFunc) HandleMessage(ctx context.Context, message *mq.MessageExt) error {
	return f(ctx, message)
}

type fakeConsumer struct {
	stop          chan int
	starts, stops int
	err           error
}

func (c *fakeConsumer) ConnectToNSQDs([]string) error { c.starts++; return c.err }
func (c *fakeConsumer) Stop()                         { c.stops++; close(c.stop) }
func testConsumer() (*Consumer, *fakeConsumer, context.Context) {
	client := &fakeConsumer{stop: make(chan int)}
	ctx, cancel := context.WithCancel(context.Background())
	return &Consumer{client: client, stop: client.stop, cancel: cancel, topic: "events"}, client, ctx
}

func TestHandlerFailureIsNotAcknowledged(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			c, _, ctx := testConsumer()
			defer c.Close()
			failure := errors.New("failure")
			c.RegisterHandler(handlerFunc(func(_ context.Context, message *mq.MessageExt) error {
				if message.Topic != "events" || message.MsgID != "0123456789abcdef" || string(message.Body) != "body" {
					t.Fatalf("message=%+v", message)
				}
				switch mode {
				case "error":
					return failure
				case "panic":
					panic("panic")
				case "cancel":
					c.cancel()
				}
				return nil
			}))
			var id nsqclient.MessageID
			copy(id[:], "0123456789abcdef")
			err := c.handle(ctx, nsqclient.NewMessage(id, []byte("body")))
			if (err != nil) != (mode != "success") {
				t.Fatalf("error=%v", err)
			}
			if mode == "error" && !errors.Is(err, failure) {
				t.Fatal(err)
			}
		})
	}
}

func TestHandlerBodySnapshot(t *testing.T) {
	c, _, ctx := testConsumer()
	defer c.Close()
	c.RegisterHandler(handlerFunc(func(_ context.Context, message *mq.MessageExt) error {
		message.Body[0] = 'X'
		return errors.New("retry")
	}))
	message := nsqclient.NewMessage(nsqclient.MessageID{}, []byte("body"))
	if err := c.handle(ctx, message); err == nil {
		t.Fatal("failure swallowed")
	}
	if string(message.Body) != "body" {
		t.Fatal("handler mutated SDK message")
	}
}

func TestHandlerTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, ctx := testConsumer()
		defer c.Close()
		c.timeout = time.Second
		c.RegisterHandler(handlerFunc(func(ctx context.Context, _ *mq.MessageExt) error { <-ctx.Done(); return nil }))
		if err := c.handle(ctx, nsqclient.NewMessage(nsqclient.MessageID{}, nil)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired handler accepted: %v", err)
		}
	})
}

func TestConsumerLifecycle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			c, client, _ := testConsumer()
			if fail {
				client.err = errors.New("connection failed")
			}
			c.RegisterHandler(handlerFunc(func(context.Context, *mq.MessageExt) error { return nil }))
			err := c.Start()
			if !errors.Is(err, client.err) {
				t.Fatalf("start error=%v", err)
			}
			if !fail {
				if err := c.Start(); err != nil {
					t.Fatal(err)
				}
			}
			if fail && client.stops != 1 {
				t.Fatal("failed startup did not clean up")
			}
			for range 2 {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if client.starts != 1 || client.stops != 1 {
				t.Fatalf("starts=%d stops=%d", client.starts, client.stops)
			}
			if err := c.Start(); !errors.Is(err, ErrClosed) {
				t.Fatal(err)
			}
		})
	}
}

func TestCloseWaitsForHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, ctx := testConsumer()
		entered, release := make(chan struct{}), make(chan struct{})
		c.RegisterHandler(handlerFunc(func(ctx context.Context, _ *mq.MessageExt) error { close(entered); <-ctx.Done(); <-release; return nil }))
		handled := make(chan error, 1)
		go func() { handled <- c.handle(ctx, nsqclient.NewMessage(nsqclient.MessageID{}, nil)) }()
		<-entered
		var closed atomic.Bool
		go func() { c.Close(); closed.Store(true) }()
		synctest.Wait()
		if closed.Load() {
			t.Error("Close returned with live handler")
		}
		close(release)
		if err := <-handled; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		synctest.Wait()
		if !closed.Load() {
			t.Fatal("Close failed to finish")
		}
	})
}
