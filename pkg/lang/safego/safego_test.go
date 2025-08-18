package safego

import (
	"context"
	"testing"
	"time"
)

func TestGo(t *testing.T) {
	t.Run("safe goroutine execution", func(t *testing.T) {
		ctx := context.Background()
		done := make(chan struct{})

		Go(ctx, func() {
			defer close(done)
		})

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("goroutine did not complete")
		}
	})

	t.Run("recover from panic in goroutine", func(t *testing.T) {
		ctx := context.Background()
		done := make(chan struct{})

		Go(ctx, func() {
			defer close(done)
			panic("test panic")
		})

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("goroutine did not complete")
		}
	})
}
