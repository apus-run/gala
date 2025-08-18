package noop

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/apus-run/gala/components/dlock"
)

// NoopLocke provides a no-operation implementation of a distributed lock.
type NoopLocke struct {
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string // Records the owner ID
}

// Ensure NoopLocke implements the Locker interface.
var _ dlock.Locker = (*NoopLocke)(nil)

// NewLock creates a new NoopLocke instance.
func NewLock(_ context.Context, opts ...dlock.Option) *NoopLocke {
	o := dlock.ApplyOptions(opts...)
	return &NoopLocke{
		lockTimeout: o.LockTimeout,
		ownerID:     o.OwnerID,
		stopChan:    make(chan struct{}),
	}
}

// Lock simulates acquiring a distributed lock.
func (l *NoopLocke) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Start the renewal goroutine
	l.renewTicker = time.NewTicker(l.lockTimeout / 2)
	go l.refreshLock(ctx)

	slog.Info("Lock acquired", "ownerID", l.ownerID)
	return nil
}

// Unlock simulates releasing a distributed lock.
func (l *NoopLocke) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Stop the renewal process
	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
	}

	slog.Info("Lock released", "ownerID", l.ownerID)
	l.ownerID = "" // Clear the owner ID
	return nil
}

// Refresh simulates refreshing the lock's expiration time.
func (l *NoopLocke) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Simulate the Refreshal operation

	slog.Info("Lock renewed", "ownerID", l.ownerID)
	return nil
}

// refreshLock periodically renews the lock.
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
