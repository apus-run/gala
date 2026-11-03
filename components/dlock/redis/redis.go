// Package redis provides a distributed lock built on a redis key with TTL |
// redis 提供基于带 TTL 的 redis 键的分布式锁.
//
// # Notes | 说明
//
// Lock runs SET NX with LockTimeout as the TTL: if the key is held by the
// same ownerID it succeeds idempotently, otherwise it fails with the current
// owner. A background goroutine refreshes the TTL every LockTimeout/2 until
// Unlock deletes the key. The stored value is the bare ownerID and Unlock
// deletes without comparing owners, so processes sharing an OwnerID (same
// host by default) can release each other's locks.
// Lock 用 SET NX 写入，TTL 为 LockTimeout：键已存在且属同一 ownerID 时幂等
// 成功，否则携当前持有者报错。后台协程每 LockTimeout/2 刷新 TTL，直到 Unlock
// 删除键。存储值只有 ownerID 本身，Unlock 删除时不比对持有者——共享 OwnerID
// （默认同主机）的进程可能互删对方的锁。
//
// # Example | 示例
//
//	locker := redis.NewLock(ctx, rdb, dlock.WithLockName("export"))
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package redis

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
)

// Lock provides a distributed locking mechanism using Redis | Lock 提供基于 Redis 的分布式锁机制。
type Lock struct {
	client      *redis.Client
	lockName    string
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance | NewLock 创建新的 Lock 实例。
func NewLock(ctx context.Context, client *redis.Client, opts ...dlock.Option) *Lock {
	o := dlock.ApplyOptions(opts...)
	locker := &Lock{
		client:      client,
		lockName:    o.LockName,
		lockTimeout: o.LockTimeout,
		stopChan:    make(chan struct{}),
		ownerID:     o.OwnerID,
	}

	slog.Info("Lock initialized", "lockName", locker.lockName, "ownerID", locker.ownerID)
	return locker
}

// Lock attempts to acquire the distributed lock | Lock 尝试获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	success, err := l.client.SetNX(ctx, l.lockName, l.ownerID, l.lockTimeout).Result()
	if err != nil {
		slog.Error("Failed to set lock", "error", err)
		return err
	}
	if !success {
		currentOwnerID, err := l.client.Get(ctx, l.lockName).Result()
		if err != nil {
			slog.Error("Failed to get current owner ID", "error", err)
			return err
		}
		if currentOwnerID != l.ownerID {
			slog.Warn("Lock is already held by another owner", "currentOwnerID", currentOwnerID)
			return fmt.Errorf("lock is already held by %s", currentOwnerID)
		}
		slog.Info("Lock is already held by the current owner, extending the lock if needed")
		return nil
	}

	l.renewTicker = time.NewTicker(l.lockTimeout / 2)
	go l.refreshLock(ctx)

	slog.Info("Lock acquired", "ownerID", l.ownerID)
	return nil
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
		slog.Info("Stopped renewing lock", "lockName", l.lockName)
	}

	err := l.client.Del(ctx, l.lockName).Err()
	if err != nil {
		slog.Error("Failed to delete lock", "error", err)
		return err
	}

	slog.Info("Lock released", "ownerID", l.ownerID)
	return nil
}

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	err := l.client.Expire(ctx, l.lockName, l.lockTimeout).Err()
	if err != nil {
		slog.Error("Failed to renew lock", "error", err)
		return err
	}

	slog.Info("Lock renewed", "ownerID", l.ownerID)
	return nil
}

// refreshLock periodically renews the lock | refreshLock 周期性续期锁。
func (l *Lock) refreshLock(ctx context.Context) {
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
