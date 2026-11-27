package dlock_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	goredis "github.com/redis/go-redis/v9"
	gormio "gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
	dlockgorm "github.com/apus-run/gala/components/dlock/gorm"
	"github.com/apus-run/gala/components/dlock/redis"
)

// TestLockerContract runs the same behaviour checks against every backend
// that can run without an external service and provides real exclusivity.
// noop is deliberately excluded (it always succeeds); etcd, Consul and
// MongoDB need their real services and are not exercised here (see README);
// gorm runs on pure-Go SQLite.
// TestLockerContract 对无需外部服务且提供真实排他性的后端执行同一组行为检查。
// noop 故意排除（它永远成功）；etcd、Consul、MongoDB 需要真实服务，这里不覆盖
// （见 README）；gorm 使用纯 Go 的 SQLite。
func TestLockerContract(t *testing.T) {
	const lockName = "contract"
	const ttl = time.Minute
	type fixture struct {
		client dlock.Client
		expire func(t *testing.T)
	}

	backends := []struct {
		name  string
		setup func(t *testing.T) fixture
	}{
		{
			name: "redis",
			setup: func(t *testing.T) fixture {
				srv := miniredis.RunT(t)
				rdb := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				return fixture{
					client: redis.NewClient(rdb),
					expire: func(t *testing.T) { srv.FastForward(ttl) },
				}
			},
		},
		{
			name: "gorm",
			setup: func(t *testing.T) fixture {
				db, err := gormio.Open(
					sqlite.Open(filepath.Join(t.TempDir(), "locks.db")),
					&gormio.Config{TranslateError: true},
				)
				if err != nil {
					t.Fatalf("gorm.Open() error = %v", err)
				}
				client := dlockgorm.NewClient(db)
				if err := client.Init(context.Background()); err != nil {
					t.Fatalf("Init() error = %v", err)
				}
				return fixture{
					client: client,
					expire: func(t *testing.T) {
						t.Helper()
						result := db.Model(&dlockgorm.GORMLock{}).Where("name = ?", lockName).
							Update("expired_at", time.Now().Add(-time.Second))
						if result.Error != nil || result.RowsAffected != 1 {
							t.Fatalf("expire lease: error = %v, rows = %d", result.Error, result.RowsAffected)
						}
					},
				}
			},
		},
	}

	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			store := backend.setup(t)

			newLocker := func(t *testing.T, owner string) dlock.Locker {
				t.Helper()
				locker, err := store.client.NewLock(ctx,
					dlock.WithLockName(lockName),
					dlock.WithLockTimeout(ttl),
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

			store.expire(t)
			if err := second.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("Refresh() after expiry, before takeover = %v, want ErrLockNotHeld", err)
			}

			successor := newLocker(t, "owner-b")
			if err := successor.Lock(ctx); err != nil {
				t.Fatalf("same-owner successor Lock() after expiry = %v", err)
			}
			if err := second.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("stale Refresh() after takeover = %v, want ErrLockNotHeld", err)
			}
			if err := second.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("stale Unlock() after takeover = %v, want ErrLockNotHeld", err)
			}
			if err := successor.Refresh(ctx); err != nil {
				t.Fatalf("successor Refresh() after stale operations = %v", err)
			}
			store.expire(t)
			if err := successor.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("Unlock() after expiry, before takeover = %v, want ErrLockNotHeld", err)
			}
		})
	}
}
