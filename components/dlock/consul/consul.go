// Package consul provides a distributed lock built on a Consul session and
// KV entry | consul 提供基于 Consul session 与 KV 条目的分布式锁.
//
// # Notes | 说明
//
// Lock creates a session whose TTL equals LockTimeout (behavior: release)
// and puts the lock key bound to that session; a background goroutine
// renews the session every LockTimeout/2 until Unlock deletes the key. If
// the holder dies, the session expires and the KV entry is released
// automatically. Locks are exclusive: acquiring a held lock fails instead of
// blocking.
// Lock 创建 TTL 等于 LockTimeout 的 session（behavior: release），并写入绑定
// 该 session 的锁 key；后台协程每 LockTimeout/2 续期一次，直到 Unlock 删除
// key。持有者死亡时 session 过期，KV 条目自动释放。锁是排他的：抢占已持有的
// 锁会报错而不是阻塞。
//
// # Example | 示例
//
//	cli := consul.NewClient(apiClient)
//	locker, _ := cli.NewLock(ctx, dlock.WithLockName("export"))
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package consul

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/apus-run/gala/components/dlock"
)

// Lock is a structure that implements distributed locking using Consul | Lock 是基于 Consul 实现分布式锁的结构体。
type Lock struct {
	client      *api.Client   // Consul client for interacting with the Consul API | 与 Consul API 交互的客户端
	lockKey     string        // Key for the distributed lock | 分布式锁的 key
	lockTimeout time.Duration // Duration for which the lock is valid | 锁的有效时长
	renewTicker *time.Ticker  // Ticker for renewing the lock periodically | 周期性续期锁的定时器
	stopChan    chan struct{} // Channel to signal stopping the renewal process | 通知停止续期流程的 channel
	mu          sync.Mutex    // Mutex for synchronizing access to the locker | 同步访问锁的互斥量
	ownerID     string        // Identifier for the owner of the lock | 锁持有者的标识
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance | NewLock 创建新的 Lock 实例。
func NewLock(ctx context.Context, client *api.Client, opts ...dlock.Option) (*Lock, error) {
	o := dlock.ApplyOptions(opts...)

	// initialize a new Lock with the provided options
	// 以给定选项初始化新的 Lock
	locker := &Lock{
		client:      client,
		lockKey:     o.LockName,
		lockTimeout: o.LockTimeout,
		stopChan:    make(chan struct{}),
		ownerID:     o.OwnerID,
	}

	slog.Info("Lock initialized", "lockKey", locker.lockKey, "ownerID", locker.ownerID)
	return locker, nil
}

// Lock attempts to acquire the distributed lock | Lock 尝试获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// create a new session for the lock with a TTL
	// 为锁创建一个带 TTL 的新会话
	session := &api.SessionEntry{
		TTL:      l.lockTimeout.String(),
		Behavior: api.SessionBehaviorRelease,
	}

	// create a session and handle any errors
	// 创建会话并处理错误
	sessionID, _, err := l.client.Session().Create(session, nil)
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		return fmt.Errorf("failed to create session: %v", err)
	}

	// create a KV pair for the lock
	// 为锁创建 KV 键值对
	kv := &api.KVPair{
		Key:     l.lockKey,
		Value:   []byte(l.ownerID),
		Session: sessionID,
	}

	// attempt to put the lock in the KV store and handle any errors
	// 尝试将锁写入 KV 存储并处理错误
	_, err = l.client.KV().Put(kv, nil)
	if err != nil {
		slog.Error("Failed to acquire lock", "error", err)
		return fmt.Errorf("failed to acquire lock: %v", err)
	}

	// start a ticker to renew the lock periodically
	// 启动定时器周期性续期锁
	l.renewTicker = time.NewTicker(l.lockTimeout / 2)
	go l.refreshLock(ctx, sessionID)

	slog.Info("Lock acquired", "ownerID", l.ownerID, "sessionID", sessionID)
	return nil
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// stop the renewal ticker if it is running
	// 若续期定时器在运行则停止它
	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
		slog.Info("Stopped renewing lock", "lockKey", l.lockKey)
	}

	// delete the lock from the KV store and handle any errors
	// 从 KV 存储删除锁并处理错误
	_, err := l.client.KV().Delete(l.lockKey, nil)
	if err != nil {
		slog.Error("Failed to release lock", "error", err)
		return fmt.Errorf("failed to release lock: %v", err)
	}

	slog.Info("Lock released", "ownerID", l.ownerID)
	return nil
}

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// renew the session associated with the lock and handle any errors
	// 续期锁关联的会话并处理错误
	_, _, err := l.client.Session().Renew(l.ownerID, nil)
	if err != nil {
		slog.Error("Failed to renew lock", "error", err)
		return fmt.Errorf("failed to renew lock: %v", err)
	}

	slog.Info("Lock renewed", "ownerID", l.ownerID)
	return nil
}

// refreshLock periodically renews the lock | refreshLock 周期性续期锁。
func (l *Lock) refreshLock(ctx context.Context, sessionID string) {
	for {
		select {
		case <-l.stopChan:
			return
		case <-l.renewTicker.C:
			if err := l.Refresh(ctx); err != nil {
				slog.Error("Failed to renew lock", "error", err)
			} else {
				slog.Info("Lock renewed", "ownerID", l.ownerID, "sessionID", sessionID)
			}
		}
	}
}
