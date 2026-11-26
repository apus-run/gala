package dlock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/redis"
)

// TestLockerContract runs the same behaviour checks against every backend
// that can run without an external service and provides real exclusivity.
// noop is deliberately excluded (it always succeeds); etcd, SQL, Consul and
// MongoDB need their real services and are not exercised here (see README).
// TestLockerContract 对无需外部服务且提供真实排他性的后端执行同一组行为检查。
// noop 故意排除（它永远成功）；etcd、SQL、Consul、MongoDB 需要真实服务，这里
// 不覆盖（见 README）。
func TestLockerContract(t *testing.T) {
	backends := []struct {
		name   string
		client func(t *testing.T) dlock.Client
	}{
		{
			name: "redis",
			client: func(t *testing.T) dlock.Client {
				srv := miniredis.RunT(t)
				rdb := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				return redis.NewClient(rdb)
			},
		},
	}

	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			client := backend.client(t)

			newLocker := func(t *testing.T, owner string) dlock.Locker {
				t.Helper()
				locker, err := client.NewLock(ctx,
					dlock.WithLockName("contract"),
					dlock.WithLockTimeout(time.Minute),
					dlock.WithOwnerID(owner),
				)
				if err != nil {
					t.Fatalf("NewLock() error = %v", err)
				}
				return locker
			}

			first := newLocker(t, "owner-a")
			second := newLocker(t, "owner-b")

			if err := first.Lock(ctx); err != nil {
				t.Fatalf("first Lock() error = %v", err)
			}
			if err := second.Lock(ctx); !errors.Is(err, dlock.ErrLocked) {
				t.Fatalf("second Lock() error = %v, want ErrLocked", err)
			}
			if err := second.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("second Unlock() error = %v, want ErrLockNotHeld", err)
			}
			if err := second.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("second Refresh() error = %v, want ErrLockNotHeld", err)
			}

			if err := first.Refresh(ctx); err != nil {
				t.Fatalf("first Refresh() error = %v", err)
			}
			if err := first.Lock(ctx); err != nil {
				t.Fatalf("first second Lock() error = %v, want idempotent success", err)
			}

			if err := first.Unlock(ctx); err != nil {
				t.Fatalf("first Unlock() error = %v", err)
			}
			if err := first.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("first second Unlock() error = %v, want ErrLockNotHeld", err)
			}

			if err := second.Lock(ctx); err != nil {
				t.Fatalf("second Lock() after release error = %v", err)
			}
		})
	}
}
