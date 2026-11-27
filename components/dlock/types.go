// Package dlock defines the distributed-lock contract: a Client creates Lockers that lock, unlock and refresh | dlock 定义分布式锁契约：Client 创建 Locker，由 Locker 加锁、解锁与续期.
//
// # Notes | 说明
//
// This package holds interfaces, options and defaults only, importing
// nothing beyond the standard library. Backends live in subpackages:
// redis, etcd, consul, gorm (MySQL/PostgreSQL), mongo, and a noop for
// wiring tests. gorm and mongo require an explicit Init(ctx) before first
// use to create the table and index. LockTimeout (default 10s) is never
// renewed automatically — long-running work must call Refresh periodically,
// or the lock expires and another holder may take over. Each Lock/Refresh
// call is also bounded by OperationTimeout (default 5s); Unlock uses its own
// 5s cleanup context. OwnerID defaults to the hostname.
// 本包只包含接口、选项与默认值，除标准库外零导入。后端在子包中实现：redis、
// etcd、consul、gorm（MySQL/PostgreSQL）、mongo，以及供接线测试用的 noop。
// gorm 与 mongo 首次使用前必须显式调用 Init(ctx) 建表/建索引。LockTimeout
// （默认 10s）不会自动续期——长任务必须周期性调用 Refresh，否则锁过期后其他
// 持有者可能接管。每次 Lock/Refresh 还受 OperationTimeout（默认 5s）限制；
// Unlock 使用独立的 5s 清理上下文。OwnerID 默认取主机名。
//
// # Ownership | 所有权
//
// Every Locker gets a random token when it is created. Redis, GORM and Mongo
// compare the stored token on Unlock and Refresh; etcd and Consul use the
// lease/session identity and additionally verify the key on Refresh. A Locker
// whose lease expired can therefore neither release nor renew the lock of the
// current holder. OwnerID is only a human-readable label; it is not proof of
// ownership and may be shared by any number of processes. Lock returns
// [ErrLocked] when another holder owns the lock; Unlock and Refresh return
// [ErrLockNotHeld] when this Locker no longer owns it.
// 每个 Locker 创建时都会生成随机令牌：redis/gorm/mongo 在 Unlock/Refresh 时
// 比对令牌，etcd/consul 依赖租约/session 身份并在 Refresh 时校验 key。租约过期
// 的旧持有者既不能释放也不能续期当前持有者的锁。OwnerID 只是可读标签，不构成
// 所有权凭据，可以被任意多个进程共用。锁被他人持有时 Lock 返回 [ErrLocked]；
// 本 Locker 已不再持有时 Unlock/Refresh 返回 [ErrLockNotHeld]。
//
// # Features | 功能
//
// - Client.NewLock builds a Locker with WithLockName / WithLockTimeout / WithOwnerID | Client.NewLock 以 WithLockName / WithLockTimeout / WithOwnerID 构建 Locker
// - Locker: Lock, Unlock, Refresh | Locker：Lock、Unlock、Refresh
//
// # Example | 示例
//
//	locker, err := client.NewLock(ctx, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* taken by another holder */ }
//	defer locker.Unlock(ctx)
package dlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultLockName is the default name used for the distributed lock | DefaultLockName 是分布式锁的默认名称。
const DefaultLockName = "gala-distributed-lock"

// DefaultOperationTimeout bounds one Lock or Refresh call when the caller does
// not configure WithOperationTimeout. It is deliberately shorter than the
// default lock TTL, so a stuck store fails the operation instead of silently
// consuming the whole lease.
// DefaultOperationTimeout 是未配置 WithOperationTimeout 时单次 Lock/Refresh 的上限。
// 它刻意短于默认锁 TTL，存储卡住时让操作失败，而不是悄悄耗掉整个租约。
const DefaultOperationTimeout = 5 * time.Second

// Client creates Locker instances for a distributed lock | Client 创建分布式锁的 Locker 实例。
type Client interface {
	// NewLock creates a new Locker instance with the specified key and options.
	// The context is kept for API compatibility and must not be used for
	// construction I/O; backends that need schema setup expose an explicit
	// Init(ctx).
	// NewLock 用指定的 key 与选项创建新的 Locker 实例。ctx 仅为 API 兼容而保留，
	// 不得用于构造期 I/O；需要建表/建索引的后端提供显式的 Init(ctx)。
	NewLock(ctx context.Context, opts ...Option) (Locker, error)
}

// Initializer is an optional capability implemented by backends that must
// create or migrate schema before locking: gorm creates/repairs its table and
// mongo creates the unique index on name. Call Init once during startup,
// after constructing the client and before the first NewLock/Lock. Lock may
// fail closed until Init succeeds; in particular mongo.Lock verifies the
// unique index on every Lock call and refuses to insert without
// it, so a missing Init returns an error instead of silently losing
// exclusivity. Factories that only know a dlock.Client can use a type
// assertion:
//
//	if init, ok := client.(dlock.Initializer); ok {
//		if err := init.Init(ctx); err != nil {
//			return err
//		}
//	}
//
// Initializer 是可选能力，由必须在加锁前建表/迁移/建索引的后端实现：gorm 建表
// 并修复旧行，mongo 建立 name 唯一索引。启动时、构造 client 之后、首次
// NewLock/Lock 之前调用一次 Init。Init 成功前 Lock 可能 fail closed；特别是
// mongo.Lock 会在每次加锁时校验该唯一索引，缺少索引时拒绝插入，
// 从而返回错误而不是静默失去排他性。只拿到 dlock.Client 的工厂可用类型断言：
//
//	if init, ok := client.(dlock.Initializer); ok {
//		if err := init.Init(ctx); err != nil {
//			return err
//		}
//	}
type Initializer interface {
	Init(ctx context.Context) error
}

// Locker is an interface that defines the methods for a distributed lock.
// It provides methods to acquire, release, and refresh a lock in a distributed system.
// Locker 是定义分布式锁方法的接口，提供在分布式系统中获取、释放与续期锁的方法。
//
// A Locker is safe for concurrent use by multiple goroutines, but Lock,
// Refresh and Unlock still need a sensible order: Refresh only while this
// Locker holds the lock, Unlock once when done.
// Locker 可被多个 goroutine 并发调用，但 Lock/Refresh/Unlock 仍需合理编排：
// 只在持锁期间 Refresh，结束时 Unlock 一次。
type Locker interface {
	// Lock attempts to acquire the lock. Implementations bound the call with
	// OperationTimeout (default 5s) in addition to the caller's context.
	// Lock 尝试获取锁。实现会在调用方 ctx 之外再用 OperationTimeout（默认 5s）
	// 限制本次调用。
	Lock(ctx context.Context) error

	// Unlock releases the previously acquired lock. Implementations should
	// perform the release with a bounded cleanup context that ignores
	// cancellation, so a canceled request or job still frees the lock.
	// Unlock 释放之前获取的锁。实现应使用忽略取消、限时的清理上下文，保证请求/任务
	// 取消后仍能释放锁。
	Unlock(ctx context.Context) error

	// Refresh updates the expiration time of the lock.
	// It should be called periodically to keep the lock active, and is bounded
	// by OperationTimeout like Lock.
	// Refresh 更新锁的过期时间，应周期性调用以保持锁有效；与 Lock 一样受
	// OperationTimeout 限制。
	Refresh(ctx context.Context) error
}

// Options holds the configuration for the distributed lock | Options 持有分布式锁的配置。
type Options struct {
	LockName         string        // Name of the lock | 锁名。
	LockTimeout      time.Duration // Duration before the lock expires | 锁过期的时长。
	OperationTimeout time.Duration // Bound for one Lock/Refresh call | 单次 Lock/Refresh 的上限。
	OwnerID          string        // Identifier for the lock owner | 锁持有者的标识。
}

// Option is a function that modifies Options | Option 是修改 Options 的选项。
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 将普通函数适配为 Option。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// NewOptions initializes Options with default values | NewOptions 以默认值初始化 Options。
func NewOptions() *Options {
	// get the hostname as the default owner ID
	// 取主机名作为默认 OwnerID
	ownerID, _ := os.Hostname()
	return &Options{
		LockName:         DefaultLockName,
		LockTimeout:      10 * time.Second,        // Default lock timeout | 默认锁超时。
		OperationTimeout: DefaultOperationTimeout, // Default operation bound | 默认操作上限。
		OwnerID:          ownerID,                 // Set the owner ID | 设置持有者 ID。
	}
}

// ApplyOptions applies a series of Option functions to configure Options | ApplyOptions 依次应用一系列 Option 来配置 Options。
func ApplyOptions(opts ...Option) *Options {
	// create a new Options instance with default values
	// 以默认值新建 Options 实例
	o := NewOptions()
	for _, opt := range opts {
		// apply each option to the Options instance
		// 将每个选项应用到 Options 实例
		opt.apply(o)
	}

	// return the configured Options
	// 返回配置好的 Options
	return o
}

// Validate reports whether the options are usable by a backend. Backends may
// impose stricter limits, such as Consul's 10s session TTL.
// Validate 检查选项能否被后端使用。具体后端可能更严格，例如 Consul 的
// session TTL 最小为 10s。
func (o *Options) Validate() error {
	if strings.TrimSpace(o.LockName) == "" {
		return errors.New("dlock: lock name must not be empty")
	}
	if o.LockTimeout < time.Millisecond {
		return fmt.Errorf("dlock: lock timeout %s is shorter than 1ms", o.LockTimeout)
	}
	if o.OperationTimeout < time.Millisecond {
		return fmt.Errorf("dlock: operation timeout %s is shorter than 1ms", o.OperationTimeout)
	}
	return nil
}

// WithLockName sets the lock name in Options | WithLockName 设置 Options 中的锁名。
func WithLockName(name string) Option {
	return OptionFunc(func(o *Options) {
		// set the lock name
		// 设置锁名
		o.LockName = name
	})
}

// WithLockTimeout sets the lock timeout in Options | WithLockTimeout 设置 Options 中的锁超时。
func WithLockTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		// set the lock timeout
		// 设置锁超时
		o.LockTimeout = timeout
	})
}

// WithOperationTimeout bounds one Lock or Refresh call, including all store
// round trips it performs. Keep it below LockTimeout so a slow call fails
// before it consumes the lease. Unlock uses its own bounded cleanup context.
// WithOperationTimeout 限制单次 Lock/Refresh（含其全部存储往返）。建议小于
// LockTimeout，让慢调用在耗尽租约前失败。Unlock 使用独立的有界清理上下文。
func WithOperationTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.OperationTimeout = timeout
	})
}

// WithOwnerID sets the diagnostic owner ID in Options. It is not used as
// proof of ownership: every Locker also gets a random token.
// WithOwnerID 设置 Options 中用于诊断的持有者标识。它不构成所有权凭据：
// 每个 Locker 还会生成随机令牌。
func WithOwnerID(ownerID string) Option {
	return OptionFunc(func(o *Options) {
		// set the owner ID
		// 设置持有者 ID
		o.OwnerID = ownerID
	})
}
