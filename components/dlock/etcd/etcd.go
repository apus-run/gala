// Package etcd provides a distributed lock built on an etcd lease and key |
// etcd 提供基于 etcd 租约与键的分布式锁.
//
// # Notes | 说明
//
// Lock grants a lease of LockTimeout seconds and puts ownerID under that
// lease; a background goroutine keep-alives the lease every LockTimeout/2
// until Unlock revokes it and deletes the key. When the holder dies the
// lease expires and the key disappears on its own. There is no take-over
// check: Put overwrites the key even while another holder owns it, so
// exclusivity must be ensured upstream.
// Lock 创建时长为 LockTimeout 秒的租约，并把 ownerID 挂在该租约下写入；后台
// 协程每 LockTimeout/2 keep-alive 一次，直到 Unlock 撤销租约并删除 key。
// 持有者死亡时租约到期，key 自行消失。没有抢占检查：即使锁被他人持有，Put
// 也会覆盖 key，排他性需在上游保证。
//
// # Example | 示例
//
//	locker := etcd.NewLock(ctx, etcdCli, dlock.WithLockName("export"))
//	if err := locker.Lock(ctx); err != nil {
//		return err
//	}
//	defer locker.Unlock(ctx)
package etcd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
)

// Lock provides a distributed locking mechanism using etcd | Lock 提供基于 etcd 的分布式锁机制。
type Lock struct {
	cli         *clientv3.Client
	lease       clientv3.Lease
	leaseID     clientv3.LeaseID
	lockKey     string
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string
}

// Ensure etcdLock implements the Locker interface | 确保 etcdLock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock initializes a new etcdLock instance | NewLock 初始化一个新的 etcdLock 实例。
func NewLock(_ context.Context, cli *clientv3.Client, opts ...dlock.Option) *Lock {
	o := dlock.ApplyOptions(opts...)

	lease := clientv3.NewLease(cli)

	locker := &Lock{
		cli:         cli,
		lease:       lease,
		lockKey:     o.LockName,
		lockTimeout: o.LockTimeout,
		stopChan:    make(chan struct{}),
		ownerID:     o.OwnerID,
	}

	return locker
}

// Lock acquires the distributed lock | Lock 获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	leaseResp, err := l.lease.Grant(ctx, int64(l.lockTimeout.Seconds()))
	if err != nil {
		return err
	}

	l.leaseID = leaseResp.ID

	_, err = l.cli.Put(ctx, l.lockKey, l.ownerID, clientv3.WithLease(leaseResp.ID))
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %v", err)
	}

	l.renewTicker = time.NewTicker(l.lockTimeout / 2)
	go l.renewLock(ctx, leaseResp.ID)

	slog.Info("Lock acquired", "lockKey", l.lockKey, "ownerID", l.ownerID)
	return nil
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
	}

	_, err := l.cli.Delete(ctx, l.lockKey)
	if err != nil {
		return err
	}

	if _, err := l.lease.Revoke(context.Background(), l.leaseID); err != nil {
		return fmt.Errorf("failed to revoke lease: %w", err)
	}

	slog.Info("Lock released", "lockKey", l.lockKey, "ownerID", l.ownerID)
	return nil
}

// Refresh refreshes the lease for the distributed lock | Refresh 为分布式锁的租约续期。
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.lease.KeepAliveOnce(ctx, l.leaseID)
	return err
}

// renewLock periodically renews the lock lease | renewLock 周期性续期锁租约。
func (l *Lock) renewLock(ctx context.Context, leaseID clientv3.LeaseID) {
	for {
		select {
		case <-l.stopChan:
			return
		case <-l.renewTicker.C:
			if err := l.Refresh(ctx); err != nil {
				slog.Error("failed to renew lock", "err", err)
			} else {
				slog.Info("Lock renewed", "lockKey", l.lockKey, "ownerID", l.ownerID)
			}
		}
	}
}
