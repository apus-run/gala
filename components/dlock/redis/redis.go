// Package redis provides a distributed lock built on a redis key with TTL |
// redis 提供基于带 TTL 的 redis 键的分布式锁.
//
// # Notes | 说明
//
// Lock checks and writes the key in one Lua script: it succeeds when the key
// is free, or when it already stores this Locker's token (a retry after an
// ambiguous timeout) and extends the TTL in the latter case. Unlock and
// Refresh compare the stored token before deleting or extending the key, so
// a Locker whose lease expired cannot release or renew the next owner's lock.
// TTLs use PX, so LockTimeout is honoured to the millisecond. No background
// renewal is started: long-running work must call Refresh before the TTL
// elapses. The go-redis client must set Options.ContextTimeoutEnabled so
// OperationTimeout (and the caller's ctx) can cancel a stalled socket read;
// otherwise go-redis applies its ReadTimeout/WriteTimeout instead.
// Lock 用一段 Lua 原子地检查并写入 key：key 空闲时成功；key 已存着本 Locker
// 的令牌（例如响应丢失后的重试）时延长 TTL 并成功。Unlock/Refresh 先比对令牌
// 再删除或续期，租约过期的旧持有者无法释放或续期新持有者的锁。TTL 使用 PX，
// LockTimeout 精确到毫秒。不会启动后台续期：长任务必须在 TTL 到期前调用 Refresh。
// go-redis 客户端必须设置 Options.ContextTimeoutEnabled，OperationTimeout
// （以及调用方 ctx）才能取消卡住的 socket 读取；否则以 ReadTimeout/WriteTimeout
// 为准。
//
// # Example | 示例
//
//	locker, err := redis.NewLock(ctx, rdb, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package redis

import (
	"context"
	_ "embed"
	"fmt"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/internal/cleanup"
	"github.com/apus-run/gala/components/dlock/internal/opctx"
	"github.com/apus-run/gala/components/dlock/internal/token"
)

var (
	//go:embed lua/lock.lua
	lockScript string

	//go:embed lua/unlock.lua
	unlockScript string

	//go:embed lua/refresh.lua
	refreshScript string
)

// Lock provides a distributed locking mechanism using Redis | Lock 提供基于 Redis 的分布式锁机制。
type Lock struct {
	client           redis.Cmdable
	lockName         string
	lockTimeout      time.Duration
	operationTimeout time.Duration
	token            string
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance | NewLock 创建新的 Lock 实例。
func NewLock(_ context.Context, client redis.Cmdable, opts ...dlock.Option) (*Lock, error) {
	if isNilClient(client) {
		return nil, fmt.Errorf("dlock/redis: nil redis client")
	}

	o := dlock.ApplyOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, err
	}

	tok, err := token.New(o.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("dlock/redis: generate owner token: %w", err)
	}

	return &Lock{
		client:           client,
		lockName:         o.LockName,
		lockTimeout:      o.LockTimeout,
		operationTimeout: o.OperationTimeout,
		token:            tok,
	}, nil
}

// Lock attempts to acquire the distributed lock | Lock 尝试获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	res, err := l.client.Eval(ctx, lockScript, []string{l.lockName}, l.token, l.lockTimeout.Milliseconds()).Text()
	if err != nil {
		return fmt.Errorf("dlock/redis: acquire lock %q: %w", l.lockName, err)
	}
	if res == "OK" {
		return nil
	}

	if holder := l.holder(ctx); holder != "" {
		return fmt.Errorf("%w: lock %q held by %q", dlock.ErrLocked, l.lockName, holder)
	}
	return fmt.Errorf("%w: lock %q", dlock.ErrLocked, l.lockName)
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	// Release is cleanup: a canceled caller context must not leave the lock
	// behind. Keep the values, ignore cancellation, bound the request.
	// 释放属于清理：调用方 ctx 取消不应让锁留下；保留值、忽略取消并限时。
	ctx, cancel := cleanup.Context(ctx)
	defer cancel()

	res, err := l.client.Eval(ctx, unlockScript, []string{l.lockName}, l.token).Int64()
	if err != nil {
		return fmt.Errorf("dlock/redis: release lock %q: %w", l.lockName, err)
	}
	if res != 1 {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	return nil
}

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
func (l *Lock) Refresh(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	res, err := l.client.Eval(ctx, refreshScript, []string{l.lockName}, l.token, l.lockTimeout.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("dlock/redis: refresh lock %q: %w", l.lockName, err)
	}
	if res != 1 {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	return nil
}

// holder reports the diagnostic owner of the current lock, best effort.
// holder 尽力报告当前锁的持有者，仅用于错误信息。
func (l *Lock) holder(ctx context.Context) string {
	value, err := l.client.Get(ctx, l.lockName).Result()
	if err != nil {
		return ""
	}
	return token.Owner(value)
}

// isNilClient also catches a typed-nil *redis.Client stored in the
// redis.Cmdable interface, which a plain nil comparison misses. A value type
// that embeds a nil *redis.Client cannot be detected here; such a wrapper
// fails on first use.
// isNilClient 还能识别 redis.Cmdable 接口里的 typed-nil *redis.Client，
// 单纯的 nil 比较识别不了。值类型内嵌 nil *redis.Client 的情况无法在这里
// 识别；这种包装器会在首次使用时失败。
func isNilClient(client redis.Cmdable) bool {
	if client == nil {
		return true
	}
	v := reflect.ValueOf(client)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
