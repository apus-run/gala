package dlock

import "errors"

var (
	// ErrLocked is returned by Lock when another owner still holds the lock.
	// Its TTL may have expired; call Lock again to take over.
	// ErrLocked 表示锁仍被其他持有者占用；对方 TTL 到期后可再次 Lock 接管。
	ErrLocked = errors.New("dlock: lock is already held")

	// ErrLockNotHeld means the Locker no longer holds the lock. SQL and MongoDB
	// also return it from Lock when the written lease has expired by the time
	// the backend acknowledges the write. The owner must stop working on the protected resource.
	// ErrLockNotHeld 表示本 Locker 已不再持有锁。GORM 和 MongoDB 的 Lock 在
	// 确认写入时租约已到期也返回此错误。
	// 持有者必须停止操作被保护的资源。
	ErrLockNotHeld = errors.New("dlock: lock is not held")

	// ErrClosed is returned when a Locker whose backend has a Close method is
	// used after Close.
	// ErrClosed 表示使用了已 Close 的 Locker（仅限提供 Close 的后端）。
	ErrClosed = errors.New("dlock: locker is closed")
)
