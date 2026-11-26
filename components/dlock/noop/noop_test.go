package noop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/noop"
)

func TestClientNewLock(t *testing.T) {
	var client dlock.Client = noop.NewClient()

	locker, err := client.NewLock(context.Background(), dlock.WithLockName("export"))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}

	ctx := context.Background()
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("second Lock() error = %v", err)
	}
	if err := locker.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if err := locker.Unlock(ctx); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if err := locker.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("second Unlock() error = %v, want ErrLockNotHeld", err)
	}
}

func TestNewLockValidation(t *testing.T) {
	if _, err := noop.NewLock(context.Background(), dlock.WithLockName("")); err == nil {
		t.Fatal("NewLock(empty name) error = nil, want validation error")
	}
}

func TestLose(t *testing.T) {
	ctx := context.Background()

	locker, err := noop.NewLock(ctx, dlock.WithLockName("lose"))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	locker.Lose()

	if err := locker.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("Refresh() after Lose() error = %v, want ErrLockNotHeld", err)
	}
	if err := locker.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("Unlock() after Lose() error = %v, want ErrLockNotHeld", err)
	}
}
