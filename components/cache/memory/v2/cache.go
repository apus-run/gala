// Package memory provides a generic in-memory cache with TTL and
// compile-time typed keys and values | memory 提供带 TTL 且键值保留编译期类型的泛型内存缓存.
//
// # Notes | 说明
//
// Cache[K, V] returns V straight from Get instead of routing through the
// any-based [cache.Cache] interface, so it does not implement that
// interface. The backing map is unbounded; New starts a background GC
// goroutine that Close must stop; TTL granularity is seconds.
// Cache[K, V] 的 Get 直接返回 V，不经 any 型的 [cache.Cache] 接口，因此不实现
// 该接口。底层 map 无上限；New 会启动后台 gc 协程，必须用 Close 停止；TTL
// 粒度为秒。
//
// # Example | 示例
//
//	c := memory.New[string, int]()
//	_ = c.Set(ctx, "k", 1, time.Minute)
//	v, err := c.Get(ctx, "k")
//	defer c.Close()
package memory

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/apus-run/gala/components/cache"
	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/internal/timer"
)

// Cache defines a concurrent-safe in-memory key-value store with expiry, the
// generic version | Cache 是并发安全、带过期机制的内存 KV 缓存（泛型版本）。
type Cache[K comparable, V any] struct {
	data       map[K]Item[V]
	done       chan struct{}
	closeOnce  sync.Once
	gcInterval time.Duration
	mux        sync.RWMutex
}

// New returns a memory cache and starts the background GC goroutine; Close must
// be called once the cache is no longer used, or the goroutine stays alive
// forever.
// New 返回一个内存缓存并启动后台 gc 协程；不再使用时必须 Close，否则协程一直存活。
func New[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	options := NewOptions(opts...)

	s := &Cache[K, V]{
		gcInterval: options.GCInterval,
		data:       options.Data,
		done:       make(chan struct{}),
	}

	// start the timestamp updater and the background garbage collector
	// 启动时间戳更新器与后台垃圾清理
	timer.StartTimeStampUpdater()
	s.gc()

	return s
}

// Get returns the value for key, with an expiry check | Get 读取缓存（带过期检查）。
func (s *Cache[K, V]) Get(ctx context.Context, key K) (V, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	var zero V
	item, ok := s.data[key]
	if !ok {
		return zero, errs.ErrKeyNotExist
	}
	// Return the zero value when expired: never hand a stale expired value to
	// the caller.
	// 过期时返回零值，不把已过期的旧值交给调用方。
	if item.Expired() {
		return zero, errs.ErrItemExpired
	}

	return item.Value(), nil
}

// GetAny returns the wrapped value | GetAny 获取封装后的值。
func (s *Cache[K, V]) GetAny(ctx context.Context, key K) (val cache.Value) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	item, ok := s.data[key]
	switch {
	case !ok:
		val.Error = errs.ErrKeyNotExist
	case item.Expired():
		val.Error = errs.ErrItemExpired
	default:
		val.Value = item.Value()
	}
	return
}

// Set stores a value with an expiry | Set 写入缓存（带过期时间）。
func (s *Cache[K, V]) Set(ctx context.Context, key K, val V, exp time.Duration) error {
	e := timer.ExpireAt(exp)

	s.mux.Lock()
	defer s.mux.Unlock()

	item := NewItem(val, e)
	s.data[key] = *item

	return nil
}

// Delete removes the given key | Delete 删除指定键。
func (s *Cache[K, V]) Delete(ctx context.Context, key K) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	delete(s.data, key)

	return nil
}

// Deletes removes keys in batch, returning how many were removed | Deletes 批量删除，
// 返回实际删除个数。
func (s *Cache[K, V]) Deletes(ctx context.Context, keys ...K) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	n := int64(0)
	for _, k := range keys {
		if _, ok := s.data[k]; ok {
			delete(s.data, k)
			n++
		}
	}

	return n, nil
}

// Len returns the number of stored keys | Len 返回缓存条数。
func (s *Cache[K, V]) Len(ctx context.Context) int {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return len(s.data)
}

// Flush clears the whole cache | Flush 清空缓存。
func (s *Cache[K, V]) Flush(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.data = make(map[K]Item[V])

	return nil
}

// Keys lists the valid (unexpired) keys | Keys 返回有效键列表。
func (s *Cache[K, V]) Keys(ctx context.Context) []K {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if len(s.data) == 0 {
		return nil
	}

	ts := timer.Timestamp()
	keys := make([]K, 0, len(s.data))

	for k, v := range s.data {
		// filter out the expired keys
		// 过滤掉已过期的 key
		if v.Exp == 0 || v.Exp > ts {
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return nil
	}

	return keys
}

// Contains reports whether the key exists and is still valid | Contains 检查键是否存在且有效。
func (s *Cache[K, V]) Contains(ctx context.Context, key K) bool {
	s.mux.RLock()
	defer s.mux.RUnlock()

	// Read lock only; deleting here would race concurrent reads. Expired
	// items are left for the GC.
	// 只拿读锁，不能在这里 delete（会与并发读写冲突）；过期项交给 gc 清理。
	v, ok := s.data[key]
	return ok && !v.Expired()
}

// Close stops the garbage collector. It is safe to call more than once.
// Close 停止后台 gc 协程；可安全重复调用。
func (s *Cache[K, V]) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}

// Conn returns a snapshot of the data. Handing out the internal map would let
// callers read and write around the lock, a data race.
// Conn 返回数据的一份快照。返回内部 map 会让调用方绕过锁读写，构成数据竞争。
func (s *Cache[K, V]) Conn() map[K]Item[V] {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return maps.Clone(s.data)
}

// String describes the cache | String 缓存状态描述。
func (s *Cache[K, V]) String() string {
	return "memory"
}

func (s *Cache[K, V]) gc() {
	go func() {
		ticker := time.NewTicker(s.gcInterval)
		defer ticker.Stop()
		// pre-allocate the slice
		// 内存预分配
		expired := make([]K, 0, 100)

		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				ts := timer.Timestamp()
				expired = expired[:0]

				// lock to read the data
				// 锁定以读取数据
				s.mux.RLock()
				for id, v := range s.data {
					if v.Exp != 0 && v.Exp < ts {
						expired = append(expired, id)
					}
				}
				s.mux.RUnlock()

				// lock to remove the expired items
				// 锁定以删除过期项
				s.mux.Lock()
				for _, id := range expired {
					if v, ok := s.data[id]; ok && v.Exp <= ts {
						delete(s.data, id)
					}
				}
				s.mux.Unlock()
			}
		}
	}()
}
