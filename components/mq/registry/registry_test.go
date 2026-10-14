package registry

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/apus-run/gala/components/mq"
)

type testWorker struct {
	cfg    func(context.Context) (*mq.ConsumerConfig, error)
	handle func(context.Context, *mq.MessageExt) error
}

func (w testWorker) ConsumerCfg(ctx context.Context) (*mq.ConsumerConfig, error) {
	if w.cfg != nil {
		return w.cfg(ctx)
	}
	return &mq.ConsumerConfig{Topic: "events"}, nil
}
func (w testWorker) HandleMessage(ctx context.Context, m *mq.MessageExt) error {
	if w.handle != nil {
		return w.handle(ctx, m)
	}
	return nil
}

type testConsumer struct {
	start   func() error
	close   func() error
	handler mq.ConsumerHandler
}

func (c *testConsumer) Start() error {
	if c.start != nil {
		return c.start()
	}
	return nil
}
func (c *testConsumer) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}
func (c *testConsumer) RegisterHandler(h mq.ConsumerHandler) { c.handler = h }

type testFactory struct {
	mq.Factory
	create func(mq.ConsumerConfig) (mq.Consumer, error)
}

func (f testFactory) NewConsumer(cfg mq.ConsumerConfig) (mq.Consumer, error) { return f.create(cfg) }

func TestStartAllRollback(t *testing.T) {
	failure := errors.New("startup failed")
	cleanup := errors.New("cleanup failed")
	for _, stage := range []string{"config", "nil_config", "create", "nil_consumer", "start", "cancel"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var closed []int
			created := 0
			f := testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) {
				id := created
				created++
				if id == 1 && stage == "create" {
					return nil, failure
				}
				if id == 1 && stage == "nil_consumer" {
					return nil, nil
				}
				return &testConsumer{
					start: func() error {
						if id == 1 && stage == "start" {
							return failure
						}
						if id == 1 && stage == "cancel" {
							cancel()
						}
						return nil
					},
					close: func() error { closed = append(closed, id); return cleanup },
				}, nil
			}}
			second := testWorker{cfg: func(context.Context) (*mq.ConsumerConfig, error) {
				if stage == "config" {
					return nil, failure
				}
				if stage == "nil_config" {
					return nil, nil
				}
				return &mq.ConsumerConfig{}, nil
			}}
			r := NewConsumerRegistry(f).Register([]mq.ConsumerWorker{testWorker{}, second})
			err := r.StartAll(ctx)
			if err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if !errors.Is(err, cleanup) {
				t.Fatalf("cleanup error lost: %v", err)
			}
			if stage == "config" || stage == "create" || stage == "start" {
				if !errors.Is(err, failure) {
					t.Fatalf("startup error lost: %v", err)
				}
			}
			if stage == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			want := []int{0}
			if stage == "start" || stage == "cancel" {
				want = []int{1, 0}
			}
			if !reflect.DeepEqual(closed, want) {
				t.Fatalf("closed %v, want %v", closed, want)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(closed, want) {
				t.Fatal("rolled back consumers closed twice")
			}
		})
	}
}

func TestRegistryLifecycle(t *testing.T) {
	var created, started int
	var closed []int
	closeErr := errors.New("close failure")
	r := NewConsumerRegistry(testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) {
		id := created
		created++
		return &testConsumer{
			start: func() error { started++; return nil },
			close: func() error { closed = append(closed, id); return closeErr },
		}, nil
	}}).Register([]mq.ConsumerWorker{testWorker{}, testWorker{}})
	for range 2 {
		if err := r.StartAll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if started != 2 || created != 2 {
		t.Fatalf("duplicate start: created=%d started=%d", created, started)
	}
	for range 2 {
		if err := r.Close(); !errors.Is(err, closeErr) {
			t.Fatalf("close error: %v", err)
		}
	}
	if !reflect.DeepEqual(closed, []int{1, 0}) {
		t.Fatalf("close order/count: %v", closed)
	}
	if err := r.StartAll(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close: %v", err)
	}
}

func TestRegistryRetryAfterRollback(t *testing.T) {
	attempt, closed := 0, 0
	r := NewConsumerRegistry(testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) {
		attempt++
		return &testConsumer{start: func() error {
			if attempt == 1 {
				return errors.New("first attempt")
			}
			return nil
		}, close: func() error { closed++; return nil }}, nil
	}}).Register([]mq.ConsumerWorker{testWorker{}})
	if err := r.StartAll(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	if err := r.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if attempt != 2 || closed != 2 {
		t.Fatalf("attempts=%d closed=%d", attempt, closed)
	}
}

func TestRegistryCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewConsumerRegistry(testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) {
		t.Fatal("created after cancellation")
		return nil, nil
	}}).Register([]mq.ConsumerWorker{testWorker{}})
	if err := r.StartAll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestRegistryConcurrentLifecycle(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	r := NewConsumerRegistry(testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) {
		return &testConsumer{start: func() error { close(entered); <-release; return nil }}, nil
	}}).Register([]mq.ConsumerWorker{testWorker{}})
	done := make(chan error, 1)
	go func() { done <- r.StartAll(context.Background()) }()
	<-entered
	startErr, closeErr := r.StartAll(context.Background()), r.Close()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(startErr, ErrBusy) || !errors.Is(closeErr, ErrBusy) {
		t.Fatalf("start=%v close=%v", startErr, closeErr)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryConcurrentRegister(t *testing.T) {
	created := 0
	r := NewConsumerRegistry(testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) {
		created++
		return &testConsumer{}, nil
	}})
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { r.Register([]mq.ConsumerWorker{testWorker{}}) })
	}
	wg.Wait()
	if err := r.StartAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if created != 32 {
		t.Fatalf("created=%d", created)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryPanicIsError(t *testing.T) {
	for _, value := range []any{"panic", errors.New("panic error"), nil} {
		t.Run("panic", func(t *testing.T) {
			var c testConsumer
			r := NewConsumerRegistry(testFactory{create: func(mq.ConsumerConfig) (mq.Consumer, error) { return &c, nil }}).
				Register([]mq.ConsumerWorker{testWorker{handle: func(context.Context, *mq.MessageExt) error { panic(value) }}})
			if err := r.StartAll(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if err := c.handler.HandleMessage(context.Background(), &mq.MessageExt{}); err == nil {
				t.Fatal("panic was acknowledged as success")
			}
		})
	}
}

func TestSafeHandlerPreservesErrorAndContext(t *testing.T) {
	failure := errors.New("business failure")
	ctx := context.Background()
	msg := &mq.MessageExt{}
	h := newSafeConsumerWrapper(testWorker{handle: func(got context.Context, m *mq.MessageExt) error {
		if got != ctx || m != msg {
			t.Fatal("handler input changed")
		}
		return failure
	}})
	if err := h.HandleMessage(ctx, msg); err != failure {
		t.Fatalf("error changed: %v", err)
	}
}
