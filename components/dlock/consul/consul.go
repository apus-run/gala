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

// Lock is a structure that implements distributed locking using Consul.
type Lock struct {
	client      *api.Client   // Consul client for interacting with the Consul API
	lockKey     string        // Key for the distributed lock
	lockTimeout time.Duration // Duration for which the lock is valid
	renewTicker *time.Ticker  // Ticker for renewing the lock periodically
	stopChan    chan struct{} // Channel to signal stopping the renewal process
	mu          sync.Mutex    // Mutex for synchronizing access to the locker
	ownerID     string        // Identifier for the owner of the lock
}

// Ensure Lock implements the Locker interface
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance.
func NewLock(ctx context.Context, client *api.Client, opts ...dlock.Option) (*Lock, error) {
	o := dlock.ApplyOptions(opts...)

	// Initialize a new Lock with the provided options
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

// Lock attempts to acquire the distributed lock.
func (l *Lock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Create a new session for the lock with a TTL
	session := &api.SessionEntry{
		TTL:      l.lockTimeout.String(),
		Behavior: api.SessionBehaviorRelease,
	}

	// Create a session and handle any errors
	sessionID, _, err := l.client.Session().Create(session, nil)
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		return fmt.Errorf("failed to create session: %v", err)
	}

	// Create a KV pair for the lock
	kv := &api.KVPair{
		Key:     l.lockKey,
		Value:   []byte(l.ownerID),
		Session: sessionID,
	}

	// Attempt to put the lock in the KV store and handle any errors
	_, err = l.client.KV().Put(kv, nil)
	if err != nil {
		slog.Error("Failed to acquire lock", "error", err)
		return fmt.Errorf("failed to acquire lock: %v", err)
	}

	// Start a ticker to renew the lock periodically
	l.renewTicker = time.NewTicker(l.lockTimeout / 2)
	go l.refreshLock(ctx, sessionID)

	slog.Info("Lock acquired", "ownerID", l.ownerID, "sessionID", sessionID)
	return nil
}

// Unlock releases the distributed lock.
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Stop the renewal ticker if it is running
	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
		slog.Info("Stopped renewing lock", "lockKey", l.lockKey)
	}

	// Delete the lock from the KV store and handle any errors
	_, err := l.client.KV().Delete(l.lockKey, nil)
	if err != nil {
		slog.Error("Failed to release lock", "error", err)
		return fmt.Errorf("failed to release lock: %v", err)
	}

	slog.Info("Lock released", "ownerID", l.ownerID)
	return nil
}

// Refresh refreshes the lock's expiration time.
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Renew the session associated with the lock and handle any errors
	_, _, err := l.client.Session().Renew(l.ownerID, nil)
	if err != nil {
		slog.Error("Failed to renew lock", "error", err)
		return fmt.Errorf("failed to renew lock: %v", err)
	}

	slog.Info("Lock renewed", "ownerID", l.ownerID)
	return nil
}

// refreshLock periodically renews the lock.
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
