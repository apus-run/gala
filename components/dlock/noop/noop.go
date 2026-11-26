// Package noop provides an in-process fake distributed lock that always
// succeeds | noop 提供永远成功的进程内假分布式锁.
//
// # Notes | 说明
//
// Implements [dlock.Locker] without touching any external store: Lock always
// succeeds, Unlock and Refresh only log. It still honours the ownership
// contract — Unlock twice or Refresh before Lock returns [dlock.ErrLockNotHeld]
// — so code that handles a lost lock can be wired and tested. You can also
// call [NoopLocke.Lose] to simulate a lost lease. Options are validated the
// same way real backends validate them. No goroutine is started and no lease
// exists; it provides no exclusivity at all.
// 实现 [dlock.Locker] 但不触碰任何外部存储：Lock 永远成功，Unlock 与 Refresh
// 只打日志。它仍遵守所有权契约——重复 Unlock 或未加锁就 Refresh 返回
// [dlock.ErrLockNotHeld]——因此处理丢锁的代码可以在接线测试中被覆盖。也可以调用
// [NoopLocke.Lose] 模拟租约丢失。选项校验与真实后端一致。不会启动协程，也没有
// 租约；完全不提供排他性。
//
// # Example | 示例
//
//	locker, err := noop.NewLock(ctx, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	_ = locker.Lock(ctx)
//	defer locker.Unlock(ctx)
package noop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/apus-run/gala/components/dlock"
)

// NoopLocke provides a no-operation implementation of a distributed lock | NoopLocke 提供分布式锁的空实现。
type NoopLocke struct {
	lockName string
	mu       sync.Mutex
	ownerID  string
	held     bool
}

// Ensure NoopLocke implements the Locker interface | 确保 NoopLocke 实现 Locker 接口。
var _ dlock.Locker = (*NoopLocke)(nil)

// NewLock creates a new NoopLocke instance | NewLock 创建新的 NoopLocke 实例。
func NewLock(_ context.Context, opts ...dlock.Option) (*NoopLocke, error) {
	o := dlock.ApplyOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return &NoopLocke{
		lockName: o.LockName,
		ownerID:  o.OwnerID,
	}, nil
}

// Lock simulates acquiring a distributed lock | Lock 模拟获取分布式锁。
func (l *NoopLocke) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.held = true
	slog.Info("Lock acquired", "lockName", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Unlock simulates releasing a distributed lock | Unlock 模拟释放分布式锁。
func (l *NoopLocke) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.held {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	l.held = false

	slog.Info("Lock released", "lockName", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Refresh simulates refreshing the lock's expiration time | Refresh 模拟刷新锁的过期时间。
func (l *NoopLocke) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.held {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}

	slog.Info("Lock renewed", "lockName", l.lockName, "ownerID", l.ownerID)
	return nil
}

// Lose simulates the lock being taken over by another owner, so tests can
// exercise the ErrLockNotHeld path.
// Lose 模拟锁被他人接管，便于测试验证 ErrLockNotHeld 分支。
func (l *NoopLocke) Lose() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.held = false
}
