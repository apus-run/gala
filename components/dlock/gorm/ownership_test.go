package gorm_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gormio "gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
	dlockgorm "github.com/apus-run/gala/components/dlock/gorm"
)

// TestLockTakeoverInvalidatesOldOwner covers the version-CAS take-over and the
// token predicates end to end: after another Locker takes over an expired row,
// the old owner's Refresh and Unlock must report ErrLockNotHeld.
// TestLockTakeoverInvalidatesOldOwner 端到端覆盖 version CAS 接管与令牌条件：
// 另一个 Locker 接管过期行后，旧持有者的 Refresh/Unlock 必须返回 ErrLockNotHeld。
func TestLockTakeoverInvalidatesOldOwner(t *testing.T) {
	ctx := context.Background()
	db, err := gormio.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "locks.db")),
		&gormio.Config{TranslateError: true},
	)
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := dlockgorm.Init(ctx, db); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	const lockName = "takeover"
	first, err := dlockgorm.NewLock(ctx, db,
		dlock.WithLockName(lockName),
		dlock.WithLockTimeout(time.Minute),
	)
	if err != nil {
		t.Fatalf("first NewLock() error = %v", err)
	}
	second, err := dlockgorm.NewLock(ctx, db,
		dlock.WithLockName(lockName),
		dlock.WithLockTimeout(time.Minute),
	)
	if err != nil {
		t.Fatalf("second NewLock() error = %v", err)
	}

	if err := first.Lock(ctx); err != nil {
		t.Fatalf("first Lock() error = %v", err)
	}

	// Expire the row deterministically instead of sleeping out the TTL.
	// 直接让行过期，避免 sleep 等 TTL。
	if err := db.Model(&dlockgorm.GORMLock{}).
		Where("name = ?", lockName).
		Update("expired_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("expire row error = %v", err)
	}

	if err := second.Lock(ctx); err != nil {
		t.Fatalf("second Lock() after expiry error = %v", err)
	}
	if err := first.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old owner Refresh() error = %v, want ErrLockNotHeld", err)
	}
	if err := first.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old owner Unlock() error = %v, want ErrLockNotHeld", err)
	}
	if err := second.Unlock(ctx); err != nil {
		t.Fatalf("new owner Unlock() error = %v", err)
	}

	var count int64
	if err := db.Model(&dlockgorm.GORMLock{}).Where("name = ?", lockName).Count(&count).Error; err != nil {
		t.Fatalf("count rows error = %v", err)
	}
	if count != 0 {
		t.Fatalf("rows after unlock = %d, want 0", count)
	}
}
