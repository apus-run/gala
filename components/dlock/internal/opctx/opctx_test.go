package opctx

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithTimeoutKeepsEarlierCallerDeadline(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()

	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want DeadlineExceeded from the caller", ctx.Err())
	}
	if deadline, _ := ctx.Deadline(); time.Until(deadline) > time.Second {
		t.Fatal("WithTimeout extended the caller's deadline")
	}
}

func TestWithTimeoutAppliesShorterTimeout(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("WithTimeout did not set a deadline")
	}
	if time.Until(deadline) > time.Second {
		t.Fatalf("deadline is %s away, want about 10ms", time.Until(deadline))
	}
}

func TestWithTimeoutWithoutCallerDeadline(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("WithTimeout did not set a deadline")
	}
}
