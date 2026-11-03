// Package mongo provides a distributed lock stored in a MongoDB collection |
// mongo 提供存储在 MongoDB 集合中的分布式锁.
//
// # Notes | 说明
//
// The lock is a name-keyed document: Lock writes it with an upsert, Unlock
// deletes it, Refresh extends expiredAt, and a background goroutine extends
// expiredAt every LockTimeout/2 between Lock and Unlock. Contention and
// expiry take-over rely on UpdateOne upsert semantics (MatchedCount drives
// the held-or-not verdict) — verify them before trusting this backend under
// contention.
// 锁是集合中以 name 为键的文档：Lock 用 upsert 写入，Unlock 删除，Refresh
// 续写 expiredAt，Lock 与 Unlock 之间由后台协程每 LockTimeout/2 续写。竞争
// 与过期接管依赖 UpdateOne 的 upsert 语义（是否持有由 MatchedCount 判定）
// ——在高竞争场景使用前请先核实该语义。
//
// # Example | 示例
//
//	locker, err := mongo.NewLock(ctx, client, collection, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	defer locker.Unlock(ctx)
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

// Lock provides a distributed locking mechanism using MongoDB | Lock 提供基于 MongoDB 的分布式锁机制。
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

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance | NewLock 创建新的 Lock 实例。
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

// Lock attempts to acquire the distributed lock | Lock 尝试获取分布式锁。
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

// Unlock releases the distributed lock | Unlock 释放分布式锁。
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

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
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

// refreshLock periodically renews the lock | refreshLock 周期性续期锁。
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
