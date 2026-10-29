package eventbus

import (
	"context"
	"testing"
	"time"
)

// handler 内部用收到的 ctx 调用 CloseContext 不能让 Publish 永久阻塞。
func TestCloseFromInsideHandlerDoesNotDeadlock(t *testing.T) {
	bus := NewEventBus(nil)
	done := make(chan struct{})

	_ = bus.Subscribe("shutdown", EventHandlerFunc(func(ctx context.Context, e *Event) error {
		_ = bus.(interface{ CloseContext(context.Context) error }).CloseContext(ctx)
		return nil
	}))

	go func() {
		defer close(done)
		_ = bus.Publish(context.Background(), NewEvent("shutdown", nil))
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish never returned: Close() called from a handler waits for its own dispatch")
	}
}
