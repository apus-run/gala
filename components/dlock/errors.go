package dlock

import "errors"

var (
	// ErrLocked is returned by Lock when another owner still holds the lock.
	// Its TTL may have expired; call Lock again to take over.
	// ErrLocked 表示锁仍被其他持有者占用；对方 TTL 到期后可再次 Lock 接管。
	ErrLocked = errors.New("dlock: lock is already held")

	// ErrLockNotHeld is returned by Unlock or Refresh when this Locker no
	// longer owns the lock, typically because the lease expired and another
	// owner took over. The owner must stop working on the protected resource.
	// ErrLockNotHeld 表示本 Locker 已不再持有锁，通常是租约过期后他人接管；
	// 持有者必须停止操作被保护的资源。
	ErrLockNotHeld = errors.New("dlock: lock is not held")

	// ErrClosed is returned when a Locker whose backend has a Close method is
	// used after Close.
	// ErrClosed 表示使用了已 Close 的 Locker（仅限提供 Close 的后端）。
	ErrClosed = errors.New("dlock: locker is closed")
)
