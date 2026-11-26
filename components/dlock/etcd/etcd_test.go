package etcd_test

import (
	"context"
	"errors"
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/etcd"
)

func TestNewLockRejectsNilClient(t *testing.T) {
	if _, err := etcd.NewLock(context.Background(), nil); err == nil {
		t.Fatal("NewLock(nil) error = nil, want error")
	}
}

func TestClosedLockerRejectsOperations(t *testing.T) {
	// A client with an unreachable endpoint is enough: Close and the closed
	// guard never touch the network.
	cli, err := clientv3.New(clientv3.Config{Endpoints: []string{"127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("clientv3.New() error = %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	locker, err := etcd.NewLock(context.Background(), cli, dlock.WithLockName("closed"))
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}

	if err := locker.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := locker.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}

	ctx := context.Background()
	if err := locker.Lock(ctx); !errors.Is(err, dlock.ErrClosed) {
		t.Fatalf("Lock() after Close() error = %v, want ErrClosed", err)
	}
	if err := locker.Refresh(ctx); !errors.Is(err, dlock.ErrClosed) {
		t.Fatalf("Refresh() after Close() error = %v, want ErrClosed", err)
	}
	if err := locker.Unlock(ctx); !errors.Is(err, dlock.ErrClosed) {
		t.Fatalf("Unlock() after Close() error = %v, want ErrClosed", err)
	}
}
