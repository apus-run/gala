// Package noop provides an in-process fake distributed lock that always
// succeeds | noop 提供永远成功的进程内假分布式锁.
//
// # Notes | 说明
//
// Implements [dlock.Locker] without touching any external store: Lock
// always succeeds and starts the renewal goroutine, Unlock stops it,
// Refresh only logs. Use it for wiring tests and local runs — it provides
// no exclusivity at all.
// 实现 [dlock.Locker] 但不触碰任何外部存储：Lock 永远成功并启动续期协程，
// Unlock 停止之，Refresh 只打日志。用于接线测试与本地运行——完全不提供排他性。
//
// # Example | 示例
//
//	locker := noop.NewLock(ctx, dlock.WithLockName("export"))
//	_ = locker.Lock(ctx)
//	defer locker.Unlock(ctx)
package noop

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/apus-run/gala/components/dlock"
)

// NoopLocke provides a no-operation implementation of a distributed lock | NoopLocke 提供分布式锁的空实现。
type NoopLocke struct {
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string // Records the owner ID | 记录持有者 ID
}

// Ensure NoopLocke implements the Locker interface | 确保 NoopLocke 实现 Locker 接口。
var _ dlock.Locker = (*NoopLocke)(nil)

// NewLock creates a new NoopLocke instance | NewLock 创建新的 NoopLocke 实例。
func NewLock(_ context.Context, opts ...dlock.Option) *NoopLocke {
	o := dlock.ApplyOptions(opts...)
	return &NoopLocke{
		lockTimeout: o.LockTimeout,
		ownerID:     o.OwnerID,
		stopChan:    make(chan struct{}),
	}
}

// Lock simulates acquiring a distributed lock | Lock 模拟获取分布式锁。
func (l *NoopLocke) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// start the renewal goroutine
	// 启动续期 goroutine
	l.renewTicker = time.NewTicker(l.lockTimeout / 2)
	go l.refreshLock(ctx)

	slog.Info("Lock acquired", "ownerID", l.ownerID)
	return nil
}

// Unlock simulates releasing a distributed lock | Unlock 模拟释放分布式锁。
func (l *NoopLocke) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// stop the renewal process
	// 停止续期流程
	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
	}

	slog.Info("Lock released", "ownerID", l.ownerID)
	l.ownerID = "" // Clear the owner ID | 清空持有者 ID
	return nil
}

// Refresh simulates refreshing the lock's expiration time | Refresh 模拟刷新锁的过期时间。
func (l *NoopLocke) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// simulate the refreshal operation
	// 模拟刷新操作

	slog.Info("Lock renewed", "ownerID", l.ownerID)
	return nil
}

// refreshLock periodically renews the lock | refreshLock 周期性续期锁。
func (l *NoopLocke) refreshLock(ctx context.Context) {
	for {
		select {
		case <-l.stopChan:
			return
		case <-l.renewTicker.C:
			if err := l.Refresh(ctx); err != nil {
				slog.Error("Failed to renew lock", "error", err)
			}
		}
	}
}
