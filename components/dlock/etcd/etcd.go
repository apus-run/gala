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

// Lock provides a distributed locking mechanism using etcd.
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

// Ensure etcdLock implements the Locker interface.
var _ dlock.Locker = (*Lock)(nil)

// NewLock initializes a new etcdLock instance.
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

// Lock acquires the distributed lock.
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

// Unlock releases the distributed lock.
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

// Refresh refreshes the lease for the distributed lock.
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.lease.KeepAliveOnce(ctx, l.leaseID)
	return err
}

// renewLock periodically renews the lock lease.
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
