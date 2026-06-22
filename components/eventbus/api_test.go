package eventbus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventBuilderAndData(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	event := NewEvent("created", map[string]any{"name": "gala"}).
		WithSource("test").
		WithPriority(3)
	event.Metadata = nil
	event.WithMetadata("trace", "trace-1")

	if event.Source != "test" || event.Priority != 3 || event.Metadata["trace"] != "trace-1" {
		t.Fatalf("event builder result = %#v", event)
	}
	var decoded payload
	if err := event.GetData(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "gala" {
		t.Fatalf("decoded payload = %#v", decoded)
	}
	if err := NewEvent("nil", nil).GetData(&decoded); err != nil {
		t.Fatalf("nil data returned error: %v", err)
	}
}

func TestChainAndFilterHandlers(t *testing.T) {
	var calls atomic.Int32
	handler := EventHandlerFunc(func(context.Context, *Event) error {
		calls.Add(1)
		return nil
	})
	chain := NewChainHandler(handler, handler)
	filter := NewFilterHandler(func(event *Event) bool { return event.Priority > 0 }, chain)

	if err := filter.Handle(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	if err := filter.Handle(t.Context(), NewEvent("test", nil).WithPriority(1)); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want 2", got)
	}
}

func TestMiddlewares(t *testing.T) {
	logger := discardLogger()
	wantErr := errors.New("failed")
	failing := EventHandlerFunc(func(context.Context, *Event) error { return wantErr })
	if err := LoggingMiddleware(logger)(failing).Handle(t.Context(), NewEvent("test", nil)); !errors.Is(err, wantErr) {
		t.Fatalf("logging middleware error = %v", err)
	}
	if err := MetricsMiddleware(logger)(failing).Handle(t.Context(), NewEvent("test", nil)); !errors.Is(err, wantErr) {
		t.Fatalf("metrics middleware error = %v", err)
	}

	recovered := RecoveryMiddleware(logger)(EventHandlerFunc(func(context.Context, *Event) error { panic("boom") }))
	var panicErr *PanicError
	if err := recovered.Handle(t.Context(), NewEvent("test", nil)); !errors.As(err, &panicErr) {
		t.Fatalf("recovery middleware error = %v", err)
	}

	var attempts atomic.Int32
	retry := RetryMiddleware(2, 0)(EventHandlerFunc(func(context.Context, *Event) error {
		if attempts.Add(1) < 3 {
			return wantErr
		}
		return nil
	}))
	if err := retry.Handle(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("retry attempts = %d, want 3", got)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	retry = RetryMiddleware(1, time.Hour)(failing)
	if err := retry.Handle(ctx, NewEvent("test", nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry error = %v", err)
	}

	chain := Chain(LoggingMiddleware(logger), MetricsMiddleware(logger))(EventHandlerFunc(func(context.Context, *Event) error { return nil }))
	if err := chain.Handle(t.Context(), NewEvent("test", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestManagerForwardingAndClosedState(t *testing.T) {
	manager := NewManager(discardLogger())
	var namedCalls atomic.Int32
	var globalCalls atomic.Int32
	namedHandler := EventHandlerFunc(func(context.Context, *Event) error {
		namedCalls.Add(1)
		return nil
	})
	globalHandler := EventHandlerFunc(func(context.Context, *Event) error {
		globalCalls.Add(1)
		return nil
	})
	if err := manager.Subscribe("named", "test", namedHandler); err != nil {
		t.Fatal(err)
	}
	if err := manager.SubscribeGlobal("test", globalHandler); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Global(); err != nil {
		t.Fatal(err)
	}

	for _, publish := range []func() error{
		func() error { return manager.Publish(t.Context(), "named", NewEvent("test", nil)) },
		func() error { return manager.PublishAsync(t.Context(), "named", NewEvent("test", nil)) },
		func() error { return manager.PublishAndWait(t.Context(), "named", NewEvent("test", nil)) },
		func() error { return manager.PublishGlobal(t.Context(), NewEvent("test", nil)) },
		func() error { return manager.PublishGlobalAsync(t.Context(), NewEvent("test", nil)) },
		func() error { return manager.PublishGlobalAndWait(t.Context(), NewEvent("test", nil)) },
	} {
		if err := publish(); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if got := namedCalls.Load(); got != 3 {
		t.Fatalf("named handler calls = %d, want 3", got)
	}
	if got := globalCalls.Load(); got != 3 {
		t.Fatalf("global handler calls = %d, want 3", got)
	}
	if _, err := manager.GetBus("after-close"); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("GetBus() error = %v, want ErrManagerClosed", err)
	}
	if _, err := manager.Global(); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("Global() error = %v, want ErrManagerClosed", err)
	}
}
