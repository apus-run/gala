package rds

import (
	"context"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/redis/go-redis/v9"
)

func TestProducerRequiresContextDeadlines(t *testing.T) {
	client := redis.NewClient(&redis.Options{})
	defer client.Close()
	f, err := NewFactory(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.NewProducer(mq.ProducerConfig{}); err == nil {
		t.Fatal("accepted client ignoring context deadlines")
	}
	if client.Options().ContextTimeoutEnabled {
		t.Fatal("borrowed client mutated")
	}
}

func TestProducerRejectsDisabledSocketDeadlines(t *testing.T) {
	for _, opts := range []*redis.Options{
		{ContextTimeoutEnabled: true, ReadTimeout: -2},
		{ContextTimeoutEnabled: true, WriteTimeout: -2},
	} {
		client := redis.NewClient(opts)
		f, err := NewFactory(client)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.NewProducer(mq.ProducerConfig{}); err == nil {
			t.Error("accepted disabled socket deadlines")
		}
		client.Close()
	}
	// -1 disables the fixed timeout but still permits context deadlines.
	client := redis.NewClient(&redis.Options{ContextTimeoutEnabled: true, ReadTimeout: -1, WriteTimeout: -1})
	defer client.Close()
	f, err := NewFactory(client)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.NewProducer(mq.ProducerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
}

type lateSuccessClient struct{ redis.Cmdable }

func (lateSuccessClient) XAdd(ctx context.Context, _ *redis.XAddArgs) *redis.StringCmd {
	<-ctx.Done()
	return redis.NewStringResult("1-0", nil)
}
func TestProducerRejectsLateSuccess(t *testing.T) {
	client, _ := setup(t)
	p := producer(t, factory(t, client))
	p.client = lateSuccessClient{}
	p.timeout = time.Millisecond
	response, err := p.Send(context.Background(), mq.NewMessage("late", []byte("body")))
	if !errors.Is(err, context.DeadlineExceeded) || response.MessageID != "" {
		t.Fatalf("late success: %v %v", response, err)
	}
}

func TestProduceTimeoutRealRedis(t *testing.T) {
	if os.Getenv("REDISSTREAM_ADDR") == "" || os.Getenv("RDS_TEST_PAUSE") != "1" {
		t.Skip("requires dedicated Redis and RDS_TEST_PAUSE=1 (pauses all writes)")
	}
	for _, mode := range []string{"send", "delay", "batch", "async", "caller deadline", "socket timeout", "async socket timeout"} {
		t.Run(mode, func(t *testing.T) {
			client, topic := setup(t)
			ctx := context.Background()
			socketTimeout := mode == "socket timeout" || mode == "async socket timeout"
			if socketTimeout {
				// A separate client preserves setup/cleanup's normal I/O budget.
				opts := *client.Options()
				opts.ReadTimeout = 80 * time.Millisecond
				opts.MaxRetries = -1 // Options() contains the normalized zero, not the input sentinel.
				client = redis.NewClient(&opts)
				t.Cleanup(func() { _ = client.Close() })
			}
			// Warm the connection so the regression exercises command I/O, not dialing.
			if err := client.Ping(ctx).Err(); err != nil {
				t.Fatal(err)
			}
			f := factory(t, client)
			timeout := 80 * time.Millisecond
			if mode == "caller deadline" || socketTimeout {
				timeout = time.Second
			}
			raw, err := f.NewProducer(mq.ProducerConfig{ProduceTimeout: timeout})
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			// Bounded even if the process crashes. Cleanup unpauses before deleting keys.
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := client.Do(cleanup, "CLIENT", "UNPAUSE").Err(); err != nil {
					t.Error(err)
				}
			})
			if err := client.Do(ctx, "CLIENT", "PAUSE", 1500, "WRITE").Err(); err != nil {
				t.Fatal(err)
			}
			callCtx := ctx
			if mode == "caller deadline" {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithTimeout(ctx, 80*time.Millisecond)
				defer cancel()
			}
			m := mq.NewMessage(topic, []byte("payload"))
			started := time.Now()
			var response mq.SendResponse
			switch mode {
			case "delay":
				response, err = raw.Send(callCtx, mq.NewDeferMessage(topic, time.Second, []byte("payload")))
			case "batch":
				response, err = raw.SendBatch(callCtx, []*mq.Message{m, m})
			case "async", "async socket timeout":
				type result struct {
					response mq.SendResponse
					err      error
				}
				done := make(chan result, 1)
				err = raw.SendAsync(callCtx, func(_ context.Context, r mq.SendResponse, e error) { done <- result{r, e} }, m)
				if err != nil {
					t.Fatal(err)
				}
				select {
				case r := <-done:
					response, err = r.response, r.err
				case <-time.After(2 * time.Second):
					t.Fatal("callback did not finish")
				}
			default:
				response, err = raw.Send(callCtx, m)
			}
			elapsed := time.Since(started)
			if socketTimeout {
				var netErr net.Error
				if !errors.As(err, &netErr) || !netErr.Timeout() || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected original socket timeout, got %T %v", err, err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected context deadline, got %v", err)
			}
			if response.MessageID != "" {
				t.Fatalf("response=%v error=%v elapsed=%s", response, err, elapsed)
			}
			if elapsed < 40*time.Millisecond || elapsed > 750*time.Millisecond {
				t.Fatalf("deadline did not bound I/O: %s", elapsed)
			}
		})
	}
}

func TestClosedProducerRejectsAllSends(t *testing.T) {
	client, topic := setup(t)
	p := producer(t, factory(t, client))
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	var callbacks atomic.Int64
	callback := func(context.Context, mq.SendResponse, error) { callbacks.Add(1) }
	for _, ctx := range []context.Context{context.Background(), canceled} {
		for range 1000 {
			for _, message := range []*mq.Message{mq.NewMessage(topic, nil), nil} {
				if _, err := p.Send(ctx, message); !errors.Is(err, ErrClosed) {
					t.Fatalf("Send: %v", err)
				}
				if _, err := p.SendBatch(ctx, []*mq.Message{message}); !errors.Is(err, ErrClosed) {
					t.Fatalf("SendBatch: %v", err)
				}
				if err := p.SendAsync(ctx, callback, message); !errors.Is(err, ErrClosed) {
					t.Fatalf("SendAsync: %v", err)
				}
			}
			if _, err := p.SendBatch(ctx, nil); !errors.Is(err, ErrClosed) {
				t.Fatalf("empty batch: %v", err)
			}
			if err := p.SendAsync(ctx, nil, nil); !errors.Is(err, ErrClosed) {
				t.Fatalf("nil callback: %v", err)
			}
		}
	}
	if callbacks.Load() != 0 {
		t.Fatal("rejected async send invoked callback")
	}
	if n, err := client.Exists(context.Background(), StreamKey(topic), DelayKey(topic)).Result(); err != nil || n != 0 {
		t.Fatalf("closed producer wrote: %d %v", n, err)
	}
}
