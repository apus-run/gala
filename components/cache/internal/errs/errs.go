// Package errs holds the sentinel errors shared by the cache implementations
// | errs 收纳各缓存实现共用的哨兵错误.
//
// Internal package; import it only from inside components/cache.
// 内部包，仅供 components/cache 内部引用。
package errs

import "errors"

var (
	// ErrItemExpired is returned by Cache.Get when the cached item has expired |
	// Cache.Get 发现缓存项已过期时返回。
	ErrItemExpired error = errors.New("item has expired")
	// ErrKeyNotExist is returned by Cache.Get and Cache.Delete when the key is
	// not found in the cache | Cache.Get 与 Cache.Delete 找不到 key 时返回。
	ErrKeyNotExist error = errors.New("key not found in cache")

	// ErrDeleteKeyFailed is returned when deleting a key fails | 删除 key 失败时返回。
	ErrDeleteKeyFailed error = errors.New("delete key failed")
	// ErrSetKeyFailed is returned when writing a key fails, e.g. an empty key or
	// a nil value | 写入 key 失败时返回，如空 key 或 nil 值。
	ErrSetKeyFailed error = errors.New("set key failed")
)
