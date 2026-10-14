package rmq

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	rconsumer "github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apus-run/gala/components/mq"
	"github.com/apus-run/gala/components/mq/registry"
)

type invalidGroupWorker struct{}

func (invalidGroupWorker) ConsumerCfg(context.Context) (*mq.ConsumerConfig, error) {
	return &mq.ConsumerConfig{Addr: []string{"127.0.0.1:9876"}, Topic: "events", ConsumerGroup: "invalid group"}, nil
}
func (invalidGroupWorker) HandleMessage(context.Context, *mq.MessageExt) error { return nil }

// The actual SDK, not a mock, must survive cleanup before its initialization.
func TestRealConsumerCloseBeforeStart(t *testing.T) {
	c, err := NewFactory().NewConsumer(mq.ConsumerConfig{Addr: []string{"127.0.0.1:9876"}, Topic: "events", ConsumerGroup: "close_before_start"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close: %v", err)
	}
}

func TestRegistryRollbackAfterRealStartFailure(t *testing.T) {
	r := registry.NewConsumerRegistry(NewFactory())
	r.Register([]mq.ConsumerWorker{invalidGroupWorker{}})
	// Retrying the registry creates a fresh instance after each failed start.
	for range 2 {
		if err := r.StartAll(context.Background()); err == nil {
			t.Fatal("invalid group accepted")
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerLifecycle(t *testing.T) {
	failure := errors.New("SDK failure")
	for _, mode := range []string{"success", "subscribe_failure", "start_failure", "shutdown_failure"} {
		t.Run(mode, func(t *testing.T) {
			sdk := &testPushConsumer{}
			switch mode {
			case "subscribe_failure":
				sdk.subscribeErr = failure
			case "start_failure":
				sdk.startErr = failure
			case "shutdown_failure":
				sdk.shutdownErr = failure
			}
			c := &Consumer{consumer: sdk, topic: "events"}
			c.RegisterHandler(handlerFunc(func(context.Context, *mq.MessageExt) error { return nil }))
			var wantStartErr error
			if sdk.subscribeErr != nil || sdk.startErr != nil {
				wantStartErr = failure
			}
			startErr := c.Start()
			if !errors.Is(startErr, wantStartErr) {
				t.Fatalf("start error=%v, want %v", startErr, wantStartErr)
			}
			if startErr == nil {
				if err := c.Start(); err != nil || sdk.starts != 1 {
					t.Fatalf("duplicate start: calls=%d err=%v", sdk.starts, err)
				}
			} else if err := c.Start(); !errors.Is(err, ErrClosed) {
				t.Fatalf("failed instance restarted: %v", err)
			}
			for range 2 {
				if err := c.Close(); !errors.Is(err, sdk.shutdownErr) {
					t.Fatalf("close error=%v", err)
				}
			}
			wantStops := 0
			if startErr == nil {
				wantStops = 1
			}
			if sdk.stops != wantStops {
				t.Fatalf("Shutdown calls=%d, want %d", sdk.stops, wantStops)
			}
		})
	}
}

func TestCloseCancelsAndDrainsHandlers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sdk := &testPushConsumer{}
		c := &Consumer{consumer: sdk, topic: "events"}
		entered := make(chan context.Context, 2)
		release := make(chan struct{})
		var calls atomic.Int32
		c.RegisterHandler(handlerFunc(func(ctx context.Context, _ *mq.MessageExt) error {
			calls.Add(1)
			entered <- ctx
			<-release
			return nil
		}))
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		message := &primitive.MessageExt{Message: *primitive.NewMessage("events", nil)}
		handled := make(chan error, 1)
		go func() {
			result, err := sdk.callback(context.Background(), message)
			if result != rconsumer.ConsumeRetryLater {
				err = errors.New("canceled handler acknowledged")
			}
			handled <- err
		}()
		ctx := <-entered
		closed := make(chan error, 1)
		go func() { closed <- c.Close() }()
		synctest.Wait()
		if ctx.Err() != context.Canceled {
			t.Error("Close did not cancel handler")
		}
		if len(closed) != 0 {
			t.Error("Close returned with live handler")
		}
		if err := c.Start(); !errors.Is(err, ErrBusy) {
			t.Errorf("Start during Close: %v", err)
		}
		if err := c.Close(); !errors.Is(err, ErrBusy) {
			t.Errorf("concurrent Close: %v", err)
		}
		close(release)
		if err := <-handled; !errors.Is(err, context.Canceled) {
			t.Errorf("handler result: %v", err)
		}
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		if result, err := sdk.callback(context.Background(), message); result != rconsumer.ConsumeRetryLater || !errors.Is(err, ErrClosed) || calls.Load() != 1 {
			t.Fatalf("delivery after Close: result=%v err=%v calls=%d", result, err, calls.Load())
		}
	})
}

func TestHandlerPreservesSDKContext(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), contextKey{}, "sdk metadata"), time.Minute)
	defer cancel()
	sdk := &testPushConsumer{}
	c := &Consumer{consumer: sdk, topic: "events"}
	defer c.Close()
	c.RegisterHandler(handlerFunc(func(got context.Context, _ *mq.MessageExt) error {
		deadline, ok := got.Deadline()
		want, _ := ctx.Deadline()
		if !ok || deadline != want || got.Value(contextKey{}) != "sdk metadata" {
			return errors.New("SDK context was lost")
		}
		cancel()
		return nil
	}))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	result, err := sdk.callback(ctx, &primitive.MessageExt{Message: *primitive.NewMessage("events", nil)})
	if result != rconsumer.ConsumeRetryLater || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after handler: result=%v err=%v", result, err)
	}
}
