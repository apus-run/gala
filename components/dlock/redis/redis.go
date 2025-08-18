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

// Lock provides a distributed locking mechanism using Redis.
type Lock struct {
	client      *redis.Client
	lockName    string
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string
}

// Ensure Lock implements the Locker interface.
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance.
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

// Lock attempts to acquire the distributed lock.
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

// Unlock releases the distributed lock.
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

// Refresh refreshes the lock's expiration time.
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

// refreshLock periodically renews the lock.
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
