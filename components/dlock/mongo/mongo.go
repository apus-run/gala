package mongo

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/apus-run/gala/components/dlock"
)

// Lock provides a distributed locking mechanism using MongoDB.
type Lock struct {
	client         *mongo.Client
	lockCollection *mongo.Collection
	lockName       string
	lockTimeout    time.Duration
	renewTicker    *time.Ticker
	stopChan       chan struct{}
	mu             sync.Mutex
	ownerID        string
}

// Ensure Lock implements the Locker interface.
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance.
func NewLock(ctx context.Context, client *mongo.Client, collection *mongo.Collection, opts ...dlock.Option) (*Lock, error) {
	o := dlock.ApplyOptions(opts...)

	locker := &Lock{
		client:         client,
		lockCollection: collection,
		lockName:       o.LockName,
		lockTimeout:    o.LockTimeout,
		stopChan:       make(chan struct{}),
		ownerID:        o.OwnerID,
	}

	slog.Info("Lock initialized", "lockName", locker.lockName, "ownerID", locker.ownerID)

	return locker, nil
}

// Lock attempts to acquire the distributed lock.
func (l *Lock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	expiredAt := now.Add(l.lockTimeout)

	filter := bson.M{"name": l.lockName}
	update := bson.M{
		"$setOnInsert": bson.M{
			"ownerID":   l.ownerID,
			"expiredAt": expiredAt,
		},
		"$set": bson.M{
			"ownerID":   l.ownerID,
			"expiredAt": expiredAt,
		},
	}

	opts := options.Update().SetUpsert(true)
	result, err := l.lockCollection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		slog.Error("Failed to acquire lock", "error", err)
		return fmt.Errorf("failed to acquire lock: %v", err)
	}

	if result.MatchedCount == 0 {
		slog.Warn("Lock is already held by another owner", "lockName", l.lockName)
		return fmt.Errorf("lock is already held by another owner")
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

	_, err := l.lockCollection.DeleteOne(ctx, bson.M{"name": l.lockName})
	if err != nil {
		slog.Error("Failed to release lock", "error", err)
		return fmt.Errorf("failed to release lock: %v", err)
	}

	slog.Info("Lock released", "ownerID", l.ownerID)
	l.ownerID = ""
	return nil
}

// Refresh refreshes the lock's expiration time.
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	expiredAt := now.Add(l.lockTimeout)

	_, err := l.lockCollection.UpdateOne(ctx, bson.M{"name": l.lockName}, bson.M{"$set": bson.M{"expiredAt": expiredAt}})
	if err != nil {
		slog.Error("Failed to refresh lock", "error", err)
		return fmt.Errorf("failed to refresh lock: %v", err)
	}

	slog.Info("Lock refreshed", "ownerID", l.ownerID)
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
				slog.Error("Failed to refresh lock", "error", err)
			}
		}
	}
}
