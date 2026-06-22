package eventbus

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublishRecoversPanicAndAggregatesErrors(t *testing.T) {
	bus := newBoundedTestBus(t, 4)
	wantErr := errors.New("handler failed")
	var calls atomic.Int32

	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		calls.Add(1)
		panic("boom")
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		calls.Add(1)
		return wantErr
	})); err != nil {
		t.Fatal(err)
	}

	err := bus.Publish(t.Context(), NewEvent("test", nil))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want wrapped %v", err, wantErr)
	}
	var panicErr *PanicError
	if !errors.As(err, &panicErr) || panicErr.Value != "boom" {
		t.Fatalf("Publish() panic error = %v, want recovered boom", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
}

func TestPublishAsyncAppliesBackpressure(t *testing.T) {
	bus := newBoundedTestBus(t, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	var firstCall atomic.Bool
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		if firstCall.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		t.Error("second handler ran after dispatch cancellation")
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- bus.PublishAsync(ctx, NewEvent("test", nil)) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("PublishAsync() error = %v, want context.Canceled", err)
	}
	close(release)
}

func TestPublishAndWaitUsesBoundedConcurrency(t *testing.T) {
	bus := newBoundedTestBus(t, 2)
	var active atomic.Int32
	var peak atomic.Int32
	for range 8 {
		if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
			current := active.Add(1)
			for {
				previous := peak.Load()
				if current <= previous || peak.CompareAndSwap(previous, current) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			return nil
		})); err != nil {
			t.Fatal(err)
		}
	}

	if err := bus.PublishAndWait(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	if got := peak.Load(); got > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", got)
	}
}

func TestCloseWaitsForAsyncHandlers(t *testing.T) {
	bus := NewEventBusWithConcurrency(discardLogger(), 1)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		close(started)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.PublishAsync(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	<-started

	closed := make(chan error, 1)
	go func() { closed <- bus.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close() returned before handler completed: %v", err)
	default:
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

func TestCloseWaitsForPublishAndWait(t *testing.T) {
	bus := NewEventBusWithConcurrency(discardLogger(), 1)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		close(started)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	published := make(chan error, 1)
	go func() { published <- bus.PublishAndWait(t.Context(), NewEvent("test", nil)) }()
	<-started

	closed := make(chan error, 1)
	go func() { closed <- bus.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close() returned before PublishAndWait completed: %v", err)
	default:
	}
	close(release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutMiddlewareUsesContextCause(t *testing.T) {
	event := NewEvent("test", nil)
	handler := TimeoutMiddleware(time.Millisecond)(EventHandlerFunc(func(ctx context.Context, _ *Event) error {
		<-ctx.Done()
		return ctx.Err()
	}))

	err := handler.Handle(t.Context(), event)
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("timeout error = %v, want *TimeoutError", err)
	}
	if timeoutErr.EventID != event.ID || timeoutErr.EventType != event.Type {
		t.Fatalf("timeout error = %#v, want event identity", timeoutErr)
	}
}

func TestNewEventBusWithConcurrencyRejectsInvalidLimit(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("constructor with zero concurrency did not panic")
		}
	}()
	NewEventBusWithConcurrency(discardLogger(), 0)
}

func TestMiddlewareRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		call func()
	}{
		{name: "zero timeout", call: func() { TimeoutMiddleware(0) }},
		{name: "negative retries", call: func() { RetryMiddleware(-1, 0) }},
		{name: "negative delay", call: func() { RetryMiddleware(1, -time.Second) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid middleware configuration did not panic")
				}
			}()
			test.call()
		})
	}
}

func TestGetEventTypesIsSorted(t *testing.T) {
	bus := newBoundedTestBus(t, 1)
	for _, eventType := range []EventType{"z", "a", "m"} {
		if err := bus.Subscribe(eventType, EventHandlerFunc(func(context.Context, *Event) error { return nil })); err != nil {
			t.Fatal(err)
		}
	}
	types := bus.(*EventBus).GetEventTypes()
	if got, want := strings.Join(types, ","), "a,m,z"; got != want {
		t.Fatalf("GetEventTypes() = %q, want %q", got, want)
	}
}

func TestUnsubscribeRejectsFunctionIdentityGuessing(t *testing.T) {
	bus := newBoundedTestBus(t, 1)
	handler := EventHandlerFunc(func(context.Context, *Event) error { return nil })
	if err := bus.Subscribe("test", handler); err != nil {
		t.Fatal(err)
	}
	if err := bus.Unsubscribe("test", handler); !errors.Is(err, ErrUnstableHandlerIdentity) {
		t.Fatalf("Unsubscribe() error = %v, want ErrUnstableHandlerIdentity", err)
	}
}

func TestConcurrentPublishRejectsReentrancy(t *testing.T) {
	bus := newBoundedTestBus(t, 1)
	innerErr := make(chan error, 1)
	if err := bus.Subscribe("outer", EventHandlerFunc(func(ctx context.Context, _ *Event) error {
		innerErr <- bus.PublishAndWait(ctx, NewEvent("inner", nil))
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.Subscribe("inner", EventHandlerFunc(func(context.Context, *Event) error { return nil })); err != nil {
		t.Fatal(err)
	}
	if err := bus.PublishAndWait(t.Context(), NewEvent("outer", nil)); err != nil {
		t.Fatal(err)
	}
	if err := <-innerErr; !errors.Is(err, ErrReentrantPublish) {
		t.Fatalf("reentrant publish error = %v, want ErrReentrantPublish", err)
	}
}

func TestPublishAndWaitIsolatesEventMetadata(t *testing.T) {
	bus := newBoundedTestBus(t, 2)
	original := NewEvent("test", nil).WithMetadata("owner", "caller")
	for _, value := range []string{"first", "second"} {
		if err := bus.Subscribe("test", EventHandlerFunc(func(_ context.Context, event *Event) error {
			event.Metadata["owner"] = value
			return nil
		})); err != nil {
			t.Fatal(err)
		}
	}
	if err := bus.PublishAndWait(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	if got := original.Metadata["owner"]; got != "caller" {
		t.Fatalf("original metadata = %q, want caller", got)
	}
}

func TestCanceledDispatchRestoresUndispatchedOnceHandler(t *testing.T) {
	bus := newBoundedTestBus(t, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	var firstCall atomic.Bool
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		if firstCall.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	var onceCalls atomic.Int32
	if err := bus.SubscribeOnce("test", EventHandlerFunc(func(context.Context, *Event) error {
		onceCalls.Add(1)
		return nil
	})); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- bus.PublishAsync(ctx, NewEvent("test", nil)) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("PublishAsync() error = %v, want context.Canceled", err)
	}
	close(release)
	if err := bus.Publish(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	if got := onceCalls.Load(); got != 1 {
		t.Fatalf("once handler calls = %d, want 1", got)
	}
}

func TestManagerCloseDoesNotHoldLockWhileDraining(t *testing.T) {
	manager := NewManager(discardLogger())
	bus, err := manager.GetBus("worker")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	allowCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
		close(started)
		<-allowCallback
		if _, err := manager.GetBus("during-close"); !errors.Is(err, ErrManagerClosed) {
			t.Errorf("GetBus() error = %v, want ErrManagerClosed", err)
		}
		close(callbackDone)
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := bus.PublishAsync(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	<-started

	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.RLock()
		isClosed := manager.closed
		manager.mu.RUnlock()
		if isClosed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Manager.Close did not mark manager closed")
		}
		time.Sleep(time.Millisecond)
	}
	close(allowCallback)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("handler deadlocked calling Manager during Close")
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

func newBoundedTestBus(t *testing.T, limit int) PubSub {
	t.Helper()
	bus := NewEventBusWithConcurrency(discardLogger(), limit)
	t.Cleanup(func() {
		if err := bus.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})
	return bus
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
