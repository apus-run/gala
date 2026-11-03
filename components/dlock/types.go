// Package dlock defines the distributed-lock contract: a Client creates Lockers that lock, unlock and refresh | dlock 定义分布式锁契约：Client 创建 Locker，由 Locker 加锁、解锁与续期.
//
// # Notes | 说明
//
// This package holds interfaces, options and defaults only, importing
// nothing beyond the standard library. Backends live in subpackages:
// redis, etcd, consul, gorm (MySQL/PostgreSQL), mongo, and a noop for
// wiring tests. LockTimeout (default 10s) is never renewed automatically —
// long-running work must call Refresh periodically, or the lock expires and
// another holder may take over. OwnerID defaults to the hostname.
// 本包只包含接口、选项与默认值，除标准库外零导入。后端在子包中实现：redis、
// etcd、consul、gorm（MySQL/PostgreSQL）、mongo，以及供接线测试用的 noop。
// LockTimeout（默认 10s）不会自动续期——长任务必须周期性调用 Refresh，否则锁
// 过期后其他持有者可能接管。OwnerID 默认取主机名。
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
	"os"
	"time"
)

// DefaultLockName is the default name used for the distributed lock | DefaultLockName 是分布式锁的默认名称。
const DefaultLockName = "gala-distributed-lock"

// Client creates Locker instances for a distributed lock | Client 创建分布式锁的 Locker 实例。
type Client interface {
	// NewLock creates a new Locker instance with the specified key and options | NewLock 用指定的 key 与选项创建新的 Locker 实例。
	NewLock(ctx context.Context, opts ...Option) (Locker, error)
}

// Locker is an interface that defines the methods for a distributed lock.
// It provides methods to acquire, release, and refresh a lock in a distributed system.
// Locker 是定义分布式锁方法的接口，提供在分布式系统中获取、释放与续期锁的方法。
type Locker interface {
	// Lock attempts to acquire the lock | Lock 尝试获取锁。
	Lock(ctx context.Context) error

	// Unlock releases the previously acquired lock | Unlock 释放之前获取的锁。
	Unlock(ctx context.Context) error

	// Refresh updates the expiration time of the lock.
	// It should be called periodically to keep the lock active.
	// Refresh 更新锁的过期时间，应周期性调用以保持锁有效。
	Refresh(ctx context.Context) error
}

// Options holds the configuration for the distributed lock | Options 持有分布式锁的配置。
type Options struct {
	LockName    string        // Name of the lock | 锁名。
	LockTimeout time.Duration // Duration before the lock expires | 锁过期的时长。
	OwnerID     string        // Identifier for the lock owner | 锁持有者的标识。
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
		LockName:    DefaultLockName,
		LockTimeout: 10 * time.Second, // Default lock timeout | 默认锁超时。
		OwnerID:     ownerID,          // Set the owner ID | 设置持有者 ID。
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

// WithOwnerID sets the owner ID in Options | WithOwnerID 设置 Options 中的持有者 ID。
func WithOwnerID(ownerID string) Option {
	return OptionFunc(func(o *Options) {
		// set the owner ID
		// 设置持有者 ID
		o.OwnerID = ownerID
	})
}
