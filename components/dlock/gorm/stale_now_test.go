package gorm_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gormio "gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
	dlockgorm "github.com/apus-run/gala/components/dlock/gorm"
)

// TestLockUsesFreshClockForRetriedInsert forces the first acquire attempt to
// fall through the duplicate-key take-over (the seeded row vanishes between
// Create and takeOver) and delays the retry longer than the lock timeout. The
// retried insert must use a fresh clock reading: reusing the reading captured
// at the start of Lock would write an already-expired lease.
// TestLockUsesFreshClockForRetriedInsert 强制第一次加锁在唯一键接管阶段落空
// （已存在的行在 Create 与 takeOver 之间消失），并让重试延迟超过锁 TTL。
// 重试插入必须使用新的时钟读数：复用 Lock 开头的读数会写入已经过期的租约。
func TestLockUsesFreshClockForRetriedInsert(t *testing.T) {
	const (
		lockName    = "fresh-clock"
		lockTimeout = 500 * time.Millisecond
		hookDelay   = 800 * time.Millisecond
	)

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

	// Seed a live row with another token so the first insert reports a
	// duplicate key.
	// 预置一条属于其他令牌的有效行，让第一次插入得到唯一键冲突。
	if err := db.Create(&dlockgorm.GORMLock{
		Name:      lockName,
		OwnerID:   "other",
		Token:     "other@token",
		Version:   1,
		ExpiredAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed row error = %v", err)
	}

	var hookErr error
	var once sync.Once
	if err := db.Callback().Query().Before("gorm:query").Register(
		"dlock:test_delay_before_takeover_read",
		func(tx *gormio.DB) {
			once.Do(func() {
				// Delete the seeded row before takeOver's First runs, making
				// the first attempt report "row vanished" and retry the insert.
				// 在 takeOver 的 First 执行前删除预置行，使第一次尝试判定"行已
				// 消失"并重试插入。
				hookErr = tx.Session(&gormio.Session{NewDB: true}).
					Exec("DELETE FROM gorm_locks").Error
				time.Sleep(hookDelay)
			})
		}); err != nil {
		t.Fatalf("register query callback error = %v", err)
	}

	locker, err := dlockgorm.NewLock(ctx, db,
		dlock.WithLockName(lockName),
		dlock.WithLockTimeout(lockTimeout),
	)
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}

	started := time.Now()
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if hookErr != nil {
		t.Fatalf("hook DELETE error = %v", hookErr)
	}

	var row dlockgorm.GORMLock
	if err := db.First(&row, "name = ?", lockName).Error; err != nil {
		t.Fatalf("read row after Lock() error = %v", err)
	}
	if row.Token == "" {
		t.Fatal("retried insert did not store this Locker's token")
	}

	now := time.Now()
	if !row.ExpiredAt.After(now) {
		t.Fatalf(
			"retried insert wrote an expired lease: ExpiredAt=%s now=%s elapsed=%s lockTimeout=%s",
			row.ExpiredAt, now, now.Sub(started), lockTimeout,
		)
	}
}

// TestLockUsesFreshClockForExtendAfterSlowRead covers the take-over read path:
// after a duplicate-key Create, takeOver must read the clock after its row read.
// A slow First would otherwise make the extend write a lease derived from the
// pre-Create reading, which can already be expired by the time Lock returns.
// TestLockUsesFreshClockForExtendAfterSlowRead 覆盖接管读取路径：唯一键冲突后，
// takeOver 必须在读出行之后再取时钟。慢速 First 会让 extend 写入基于插入前读数
// 的租约，Lock 返回时可能已经过期。
func TestLockUsesFreshClockForExtendAfterSlowRead(t *testing.T) {
	const (
		lockName    = "fresh-clock-extend"
		lockTimeout = 500 * time.Millisecond
		hookDelay   = 800 * time.Millisecond
	)

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

	locker, err := dlockgorm.NewLock(ctx, db,
		dlock.WithLockName(lockName),
		dlock.WithLockTimeout(lockTimeout),
	)
	if err != nil {
		t.Fatalf("NewLock() error = %v", err)
	}
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("first Lock() error = %v", err)
	}

	// Delay only the next takeOver row read. The second Lock's Create hits the
	// duplicate key, then takeOver's First is slowed beyond the lease TTL.
	// 只延迟下一次 takeOver 的行读取。第二次 Lock 的 Create 命中唯一键，随后
	// takeOver 的 First 被拖慢到超过租约 TTL。
	var once sync.Once
	if err := db.Callback().Query().Before("gorm:query").Register(
		"dlock:test_delay_before_extend_read",
		func(*gormio.DB) {
			once.Do(func() { time.Sleep(hookDelay) })
		}); err != nil {
		t.Fatalf("register query callback error = %v", err)
	}

	started := time.Now()
	if err := locker.Lock(ctx); err != nil {
		t.Fatalf("second Lock() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < hookDelay {
		t.Fatalf("takeOver read was not delayed: elapsed=%s want >= %s", elapsed, hookDelay)
	}

	var row dlockgorm.GORMLock
	if err := db.First(&row, "name = ?", lockName).Error; err != nil {
		t.Fatalf("read row after extend error = %v", err)
	}
	if !row.ExpiredAt.After(time.Now()) {
		t.Fatalf(
			"extend wrote an expired lease: ExpiredAt=%s now=%s lockTimeout=%s",
			row.ExpiredAt, time.Now(), lockTimeout,
		)
	}
}
