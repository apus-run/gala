package eventbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func TestPublishContinuesAfterHandlerError(t *testing.T) {
	bus := newTestEventBus()
	wantErr := errors.New("handler failed")
	var called atomic.Int32

	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		return wantErr
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		called.Add(1)
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	err := bus.Publish(context.Background(), NewEvent("test", nil))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want %v", err, wantErr)
	}
	if got := called.Load(); got != 1 {
		t.Fatalf("second handler called %d times, want 1", got)
	}
}

func TestPublishAllowsHandlerToSubscribe(t *testing.T) {
	bus := newTestEventBus()
	done := make(chan error, 1)

	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		done <- bus.Subscribe("other", EventHandlerFunc(func(context.Context, *Event) error {
			return nil
		}))
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	go func() {
		if err := bus.Publish(context.Background(), NewEvent("test", nil)); err != nil {
			done <- err
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish deadlocked while a handler subscribed")
	}
}

func TestSubscribeOnceConcurrent(t *testing.T) {
	bus := newTestEventBus()
	var called atomic.Int32

	if err := bus.SubscribeOnce("test", EventHandlerFunc(func(context.Context, *Event) error {
		called.Add(1)
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := bus.Publish(context.Background(), NewEvent("test", nil)); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()

	if got := called.Load(); got != 1 {
		t.Fatalf("once handler called %d times, want 1", got)
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := newTestEventBus()
	var called atomic.Int32
	handler := EventHandlerFunc(func(context.Context, *Event) error {
		called.Add(1)
		return nil
	})

	if err := bus.Subscribe("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Unsubscribe("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	if got := called.Load(); got != 0 {
		t.Fatalf("unsubscribed handler called %d times, want 0", got)
	}
}

func TestUnsubscribeOnce(t *testing.T) {
	bus := newTestEventBus()
	handler := EventHandlerFunc(func(context.Context, *Event) error {
		t.Fatal("unsubscribed once handler was called")
		return nil
	})

	if err := bus.SubscribeOnce("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Unsubscribe("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestPublishAsyncPreservesContextValues(t *testing.T) {
	type contextKey struct{}

	bus := newTestEventBus()
	values := make(chan string, 1)
	if err := bus.Subscribe("test", EventHandlerFunc(func(ctx context.Context, _ *Event) error {
		values <- ctx.Value(contextKey{}).(string)
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "value"))
	cancel()
	if err := bus.PublishAsync(ctx, NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-values:
		if got != "value" {
			t.Fatalf("context value = %q, want value", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async publish")
	}
}

func TestSubscribeAsyncRejectsNilHandler(t *testing.T) {
	bus := newTestEventBus()
	if err := bus.SubscribeAsync("test", nil); err == nil {
		t.Fatal("SubscribeAsync() error = nil, want an error")
	}
}

func TestUnsubscribeAsync(t *testing.T) {
	bus := newTestEventBus()
	called := make(chan struct{}, 1)
	handler := EventHandlerFunc(func(context.Context, *Event) error {
		called <- struct{}{}
		return nil
	})

	if err := bus.SubscribeAsync("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Unsubscribe("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
		t.Fatal("unsubscribed async handler was called")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestSubscribeAsyncLogsHandlerError(t *testing.T) {
	var logs synchronizedBuffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	bus := NewEventBus(logger)
	done := make(chan struct{})

	if err := bus.SubscribeAsync("test", EventHandlerFunc(func(context.Context, *Event) error {
		defer close(done)
		return errors.New("async failure")
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async handler")
	}

	for range 100 {
		if strings.Contains(logs.String(), "Async handler error") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("async handler error was not logged: %s", logs.String())
}

func TestManagerDoesNotCreateBusAfterClose(t *testing.T) {
	manager := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	err := manager.Publish(context.Background(), "created-after-close", NewEvent("test", nil))
	if err == nil {
		t.Fatal("Publish() error = nil after manager close")
	}
	if stats := manager.GetStats(); stats["total_buses"] != 0 {
		t.Fatalf("total_buses = %v after manager close, want 0", stats["total_buses"])
	}
}

func TestManagerStatsUseGlobalBusKey(t *testing.T) {
	manager := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer manager.Close()

	stats := manager.GetStats()
	if _, ok := stats["global_bus"]; !ok {
		t.Fatal("GetStats() is missing global_bus")
	}
	if _, ok := stats["eb_bus"]; ok {
		t.Fatal("GetStats() contains unexpected eb_bus")
	}
}

func newTestEventBus() PubSub {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewEventBus(logger)
}
