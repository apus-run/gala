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
	"sync"
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

	index *indexCache
}

// indexCache records a successful unique-index verification. Client-created
// Lockers share one cache, so the listIndexes check runs once per client
// rather than once per Run; a direct NewLock gets its own cache.
// indexCache 记录一次成功的唯一索引校验。由 Client 创建的 Locker 共享同一个缓存，
// 因此 listIndexes 每个 client 只执行一次，而不是每个 Run 一次；直接调用 NewLock
// 则各自持有缓存。
type indexCache struct {
	mu    sync.Mutex
	ready bool
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
	return newLock(collection, &indexCache{}, opts...)
}

// newLock builds a Locker with the provided index-verification cache.
// newLock 用给定的索引校验缓存构建 Locker。
func newLock(collection *mongo.Collection, index *indexCache, opts ...dlock.Option) (*Lock, error) {
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
		index:            index,
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

	// Without the unique index created by Init two contenders can both insert
	// their own document for the same name, so Lock must fail closed instead of
	// silently losing exclusivity.
	// 缺少 Init 建立的唯一索引时，两个竞争者可能各自插入同名文档；Lock 必须
	// fail closed，而不是静默失去排他性。
	if err := l.requireUniqueNameIndex(ctx); err != nil {
		return err
	}

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

// requireUniqueNameIndex verifies once per Locker that Init created the unique
// index on name. A successful verification is cached; failures, including
// transient list-index errors, are not, so the next Lock retries and can
// recover after Init runs or the store comes back.
// requireUniqueNameIndex 对每个 Locker 只成功缓存一次 name 唯一索引校验；失败
// （含列出索引时的暂时性错误）不缓存，下一次 Lock 会重试，从而能在 Init 执行或
// 存储恢复后继续工作。
func (l *Lock) requireUniqueNameIndex(ctx context.Context) error {
	l.index.mu.Lock()
	defer l.index.mu.Unlock()

	if l.index.ready {
		return nil
	}

	// List raw index specs: driver v1.17's IndexSpecification drops
	// partialFilterExpression, and a partial unique index on name does not
	// constrain lock documents that miss the filter, so it must not pass the
	// fail-closed check.
	// 列出原始索引文档：driver v1.17 的 IndexSpecification 会丢弃
	// partialFilterExpression，而带 partial filter 的 name 唯一索引并不会约束
	// 不满足 filter 的锁文档，因此不能让这种索引通过 fail-closed 校验。
	cursor, err := l.collection.Indexes().List(ctx)
	if err != nil {
		return fmt.Errorf("dlock/mongo: inspect indexes on %q: %w", l.collection.Name(), err)
	}
	var specs []bson.M
	if err := cursor.All(ctx, &specs); err != nil {
		return fmt.Errorf("dlock/mongo: decode indexes on %q: %w", l.collection.Name(), err)
	}
	if !hasUniqueNameIndex(specs) {
		return fmt.Errorf(
			"dlock/mongo: collection %q has no unique index on name; call mongo.Init(ctx, collection) before Lock",
			l.collection.Name(),
		)
	}

	l.index.ready = true
	return nil
}

// hasUniqueNameIndex reports whether specs contain a unique, non-partial index
// whose only key is name in ascending order, the index Init creates.
// hasUniqueNameIndex 判断 specs 中是否存在 Init 所建立的唯一、非 partial、唯一键
// 仅为 name（升序）的索引。
func hasUniqueNameIndex(specs []bson.M) bool {
	for _, spec := range specs {
		unique, _ := spec["unique"].(bool)
		if !unique {
			continue
		}
		// A partial unique index does not apply to documents missing the
		// filter; lock documents have no such field, so it grants no
		// exclusivity for them.
		// partial 唯一索引不约束缺少 filter 字段的文档；锁文档没有该字段，因此
		// 对它们不提供任何排他性。
		if _, partial := spec["partialFilterExpression"]; partial {
			continue
		}
		keys, ok := spec["key"].(bson.M)
		if !ok || len(keys) != 1 {
			continue
		}
		if !isOne(keys["name"]) {
			continue
		}
		return true
	}
	return false
}

// isOne reports whether v is the BSON numeric 1 (int32 or int64).
// isOne 判断 v 是否为 BSON 数字 1（int32 或 int64）。
func isOne(v any) bool {
	switch n := v.(type) {
	case int32:
		return n == 1
	case int64:
		return n == 1
	default:
		return false
	}
}

// tryAcquire inserts the lock document, falling back to an atomic take-over.
// done is false when the document vanished and the caller should retry.
// tryAcquire 插入锁文档，失败时回退到原子接管。文档消失、调用方应重试时
// done 为 false。
func (l *Lock) tryAcquire(ctx context.Context) (done bool, err error) {
	insertedAt := time.Now()

	_, err = l.collection.InsertOne(ctx, lockDoc{
		Name:      l.lockName,
		OwnerID:   l.ownerID,
		Token:     l.token,
		ExpiredAt: insertedAt.Add(l.lockTimeout),
		CreatedAt: insertedAt,
		UpdatedAt: insertedAt,
	})
	if err == nil {
		return true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return true, fmt.Errorf("dlock/mongo: acquire lock %q: %w", l.lockName, err)
	}

	// Read the clock after the insert round trip: a slow primary failover
	// would otherwise shorten (or even expire) the lease written by the
	// take-over, while Lock still reports success.
	// 在插入往返之后再取时钟：否则慢速主节点切换会缩短（甚至写过期）接管写入的
	// 租约，而 Lock 仍报告成功。
	now := time.Now()

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
		// The lease is expired (or already gone): it is not held anymore.
		// Best-effort delete an expired document that still carries our
		// token, so an abandoned lock does not accumulate forever; the token
		// predicate guarantees we never touch another owner's document.
		// 租约已过期（或已消失）：不再持有。尽力删除仍带本令牌的过期文档，
		// 避免废弃锁永久堆积；令牌条件保证不会碰到其他持有者的文档。
		_, _ = l.collection.DeleteOne(ctx, bson.M{
			"name":  l.lockName,
			"token": l.token,
		})
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
