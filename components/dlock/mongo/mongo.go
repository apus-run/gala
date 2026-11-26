// Package mongo provides a distributed lock stored in a MongoDB collection |
// mongo 提供存储在 MongoDB 集合中的分布式锁.
//
// # Notes | 说明
//
// [Init] creates the unique index on name, and runs before the first lock
// operation. Lock inserts the document; on a duplicate key it atomically takes
// over only when the stored token is its own or the document has expired, so a
// live lock is never overwritten. Unlock deletes only an unexpired document
// with the matching token, and Refresh extends expiredAt only while the token
// matches and the document has not expired. No background renewal is started:
// long-running work must call Refresh before LockTimeout elapses. Unlock
// ignores caller cancellation, so a canceled request or job still releases the
// document. Expiry uses the application clock; with skewed node clocks a
// contender can take over a live lock early, so keep clocks synchronized (NTP)
// or allow a margin.
// [Init] 建立 name 唯一索引，在首次加锁前执行。Lock 插入文档；唯一键冲突时，仅当
// 令牌是自己的或文档已过期才原子接管，绝不会覆盖仍有效的锁。Unlock 只删除令牌
// 匹配且未过期的文档，Refresh 仅在令牌匹配且未过期时续写 expiredAt。不会启动
// 后台续期：长任务必须在 LockTimeout 到期前调用 Refresh。Unlock 忽略调用方取消，
// 请求/任务取消后仍会释放文档。过期判断使用应用时钟；节点时钟偏移时竞争者可能
// 提前接管仍有效的锁，请保持时钟同步（NTP）或留出余量。
//
// # Example | 示例
//
//	if err := mongo.Init(ctx, collection); err != nil { // unique index on name
//		return err
//	}
//	locker, err := mongo.NewLock(ctx, client, collection, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/internal/cleanup"
	"github.com/apus-run/gala/components/dlock/internal/opctx"
	"github.com/apus-run/gala/components/dlock/internal/token"
)

// Lock provides a distributed locking mechanism using MongoDB | Lock 提供基于 MongoDB 的分布式锁机制。
type Lock struct {
	collection       *mongo.Collection
	lockName         string
	lockTimeout      time.Duration
	operationTimeout time.Duration
	ownerID          string
	token            string
}

// lockDoc is the document stored in the collection | lockDoc 是集合中存储的文档。
type lockDoc struct {
	Name      string    `bson:"name"`
	OwnerID   string    `bson:"ownerID"`
	Token     string    `bson:"token"`
	ExpiredAt time.Time `bson:"expiredAt"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance. It performs no I/O; call [Init] once
// before the first Lock/Unlock/Refresh to create the unique index on name.
// NewLock 创建新的 Lock 实例，构造期不做任何 I/O；首次 Lock/Unlock/Refresh
// 之前调用一次 [Init] 建立 name 唯一索引。
func NewLock(_ context.Context, _ *mongo.Client, collection *mongo.Collection, opts ...dlock.Option) (*Lock, error) {
	if collection == nil {
		return nil, fmt.Errorf("dlock/mongo: nil collection")
	}

	o := dlock.ApplyOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, err
	}

	tok, err := token.New(o.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("dlock/mongo: generate owner token: %w", err)
	}

	return &Lock{
		collection:       collection,
		lockName:         o.LockName,
		lockTimeout:      o.LockTimeout,
		operationTimeout: o.OperationTimeout,
		ownerID:          o.OwnerID,
		token:            tok,
	}, nil
}

// Init creates the unique index on name the lock protocol relies on. Call it
// once during startup; running it concurrently is harmless but wasteful.
// Init 建立锁协议依赖的 name 唯一索引。启动时调用一次；并发调用不会出错但会浪费。
func Init(ctx context.Context, collection *mongo.Collection) error {
	if collection == nil {
		return fmt.Errorf("dlock/mongo: nil collection")
	}
	if _, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return fmt.Errorf("dlock/mongo: create unique index on name: %w", err)
	}
	return nil
}

// Lock attempts to acquire the distributed lock | Lock 尝试获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	// One retry covers the row/document vanishing between our insert and the
	// take-over, which means the lock became free.
	// 一次重试覆盖"插入与接管之间文档消失"的情况，此时锁已空闲。
	for range 2 {
		done, err := l.tryAcquire(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	if holder := l.holder(ctx); holder != "" {
		return fmt.Errorf("%w: lock %q held by %q", dlock.ErrLocked, l.lockName, holder)
	}
	return fmt.Errorf("%w: lock %q", dlock.ErrLocked, l.lockName)
}

// tryAcquire inserts the lock document, falling back to an atomic take-over.
// done is false when the document vanished and the caller should retry.
// tryAcquire 插入锁文档，失败时回退到原子接管。文档消失、调用方应重试时
// done 为 false。
func (l *Lock) tryAcquire(ctx context.Context) (done bool, err error) {
	now := time.Now()

	_, err = l.collection.InsertOne(ctx, lockDoc{
		Name:      l.lockName,
		OwnerID:   l.ownerID,
		Token:     l.token,
		ExpiredAt: now.Add(l.lockTimeout),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err == nil {
		return true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return true, fmt.Errorf("dlock/mongo: acquire lock %q: %w", l.lockName, err)
	}

	// Take over only our own document or an expired one. The find-and-update
	// is atomic, so at most one contender wins.
	// 仅接管自己的或已过期的文档。find-and-update 是原子的，最多一个竞争者成功。
	filter := bson.M{
		"name": l.lockName,
		"$or": bson.A{
			bson.M{"token": l.token},
			bson.M{"expiredAt": bson.M{"$lte": now}},
		},
	}
	update := bson.M{"$set": bson.M{
		"ownerID":   l.ownerID,
		"token":     l.token,
		"expiredAt": now.Add(l.lockTimeout),
		"updatedAt": now,
	}}

	var doc lockDoc
	err = l.collection.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return true, fmt.Errorf("dlock/mongo: take over lock %q: %w", l.lockName, err)
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	// Release is cleanup: a canceled caller context must not leave the
	// document behind. Keep the values, ignore cancellation, bound the
	// request.
	// 释放属于清理：调用方 ctx 取消不应让文档留下；保留值、忽略取消并限时。
	ctx, cancel := cleanup.Context(ctx)
	defer cancel()

	now := time.Now()
	res, err := l.collection.DeleteOne(ctx, bson.M{
		"name":  l.lockName,
		"token": l.token,
		// expiredAt > now keeps Unlock consistent with Refresh and the
		// Redis/etcd/Consul backends: an expired lease is not held anymore.
		// expiredAt > now 让 Unlock 与 Refresh 以及 Redis/etcd/Consul 后端
		// 一致：已过期的租约不再算持有。
		"expiredAt": bson.M{"$gt": now},
	})
	if err != nil {
		return fmt.Errorf("dlock/mongo: release lock %q: %w", l.lockName, err)
	}
	if res.DeletedCount == 0 {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	return nil
}

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
func (l *Lock) Refresh(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	now := time.Now()

	res, err := l.collection.UpdateOne(ctx,
		bson.M{
			"name":      l.lockName,
			"token":     l.token,
			"expiredAt": bson.M{"$gt": now},
		},
		bson.M{"$set": bson.M{
			"expiredAt": now.Add(l.lockTimeout),
			"updatedAt": now,
		}})
	if err != nil {
		return fmt.Errorf("dlock/mongo: refresh lock %q: %w", l.lockName, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	return nil
}

// holder reports the diagnostic owner of the current lock, best effort.
// holder 尽力报告当前锁的持有者，仅用于错误信息。
func (l *Lock) holder(ctx context.Context) string {
	var doc lockDoc
	if err := l.collection.FindOne(ctx, bson.M{"name": l.lockName}).Decode(&doc); err != nil {
		return ""
	}
	return doc.OwnerID
}
