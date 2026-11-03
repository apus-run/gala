// Package lru provides a generic LRU cache with TTL and compile-time typed
// keys and values | lru 提供带 TTL 且键值保留编译期类型的泛型 LRU 缓存.
//
// # Notes | 说明
//
// Cache[K, V] returns V straight from Get instead of routing through the
// any-based [cache.Cache] interface, so it does not implement that
// interface. Capacity comes from WithSize; New starts a background GC
// goroutine that Close must stop; TTL granularity is seconds.
// Cache[K, V] 的 Get 直接返回 V，不经 any 型的 [cache.Cache] 接口，因此不实现
// 该接口。容量经 WithSize 配置；New 会启动后台 gc 协程，必须用 Close 停止；
// TTL 粒度为秒。
//
// # Example | 示例
//
//	c := lru.New[string, int](lru.WithSize[string, int](1024))
//	_ = c.Set(ctx, "k", 1, time.Minute)
//	v, err := c.Get(ctx, "k")
//	defer c.Close()
package lru

import (
	"context"
	"sync"
	"time"

	"github.com/apus-run/gala/components/cache"
	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/internal/timer"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Cache is a generic LRU cache with expiry, the typed counterpart of the
// cache.Cache interface | 带 TTL 的 LRU 缓存（泛型版本）。
type Cache[K comparable, V any] struct {
	data       *lru.Cache[K, *Item[V]]
	mux        sync.RWMutex
	gcInterval time.Duration
	done       chan struct{}
	closeOnce  sync.Once
}

// New creates a generic LRU cache with expiry and starts the background GC
// goroutine; Close must be called once the cache is no longer used, or the
// goroutine stays alive forever.
// New 创建带过期机制的 LRU 缓存（泛型）并启动后台 gc 协程；不再使用时必须 Close，否则协程一直存活。
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

// Set stores a value with an expiry | Set 写入缓存（带过期时间）。
func (s *Cache[K, V]) Set(ctx context.Context, key K, val V, exp time.Duration) error {
	// A zero-value key cannot be written: fail loudly instead of pretending
	// success.
	// 零值 key 写不进去：报错而不是假装成功。
	var zeroK K
	if key == zeroK {
		return errs.ErrSetKeyFailed
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	e := timer.ExpireAt(exp)

	item := &Item[V]{
		Val: val,
		Exp: e,
	}

	s.data.Add(key, item)
	return nil
}

// Get returns the value for key, with an expiry check | Get 读取缓存（带过期检查）。
func (s *Cache[K, V]) Get(ctx context.Context, key K) (V, error) {
	var zeroV V
	var zeroK K
	if key == zeroK {
		return zeroV, errs.ErrKeyNotExist
	}

	s.mux.RLock()
	item, ok := s.data.Get(key)
	s.mux.RUnlock()

	if !ok {
		return zeroV, errs.ErrKeyNotExist
	}

	// Expired items are left for the background GC to remove: an async Delete
	// here could delete a fresh value rewritten in the meantime.
	// 过期项留给后台 gc 删除：在这里异步 Delete 会删掉期间重新写入的新值。
	if item.Expired() {
		return zeroV, errs.ErrItemExpired
	}
	return item.Value(), nil
}

// GetAny returns the wrapped value | GetAny 获取封装后的值。
func (s *Cache[K, V]) GetAny(ctx context.Context, key K) (val cache.Value) {
	var zeroK K
	if key == zeroK {
		val.Error = errs.ErrKeyNotExist
		return
	}

	// No lock here: Get grabs the read lock itself, and re-entering a read
	// lock deadlocks when a writer is queued.
	// 不在这里加锁：Get 自己会拿读锁，读锁重入在有写者排队时会死锁。
	value, err := s.Get(ctx, key)
	if err != nil {
		val.Error = err
	} else {
		val.Value = value
	}
	return
}

// Delete removes the given key | Delete 删除指定键。
func (s *Cache[K, V]) Delete(ctx context.Context, key K) error {
	var zeroK K
	if key == zeroK {
		return errs.ErrKeyNotExist
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	if _, ok := s.data.Peek(key); !ok {
		return errs.ErrKeyNotExist
	}

	if s.data.Remove(key) {
		return nil
	}
	return errs.ErrKeyNotExist
}

// Deletes removes keys in batch, returning how many were removed | Deletes 批量删除，
// 返回实际删除个数。
func (s *Cache[K, V]) Deletes(ctx context.Context, keys ...K) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	var count int64
	s.mux.Lock()
	defer s.mux.Unlock()

	for _, key := range keys {
		if _, ok := s.data.Peek(key); ok {
			s.data.Remove(key)
			count++
		}
	}
	return count, nil
}

// Flush purges the whole cache | Flush 清空缓存。
func (s *Cache[K, V]) Flush(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.data.Purge()
	return nil
}

// Keys lists the valid (unexpired) keys | Keys 返回有效键列表。
func (s *Cache[K, V]) Keys(ctx context.Context) []K {
	s.mux.RLock()
	defer s.mux.RUnlock()

	keys := s.data.Keys()
	// nil when no key is valid, matching memory and redis
	// 没有有效 key 时返回 nil，与 memory、redis 一致
	var result []K
	for _, k := range keys {
		if item, ok := s.data.Peek(k); ok && !item.Expired() {
			result = append(result, k)
		}
	}
	return result
}

// Contains reports whether the key exists and is still valid | Contains 检查键是否存在且有效。
func (s *Cache[K, V]) Contains(ctx context.Context, key K) bool {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if item, ok := s.data.Peek(key); ok {
		return !item.Expired()
	}
	return false
}

// String describes the cache | String 缓存状态描述。
func (s *Cache[K, V]) String() string {
	return "lru"
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
				keys := s.data.Keys()
				for _, key := range keys {
					if item, ok := s.data.Peek(key); ok && item.IsExpired(ts) {
						expired = append(expired, key)
					}
				}
				s.mux.RUnlock()

				// lock to remove the expired items
				// 锁定以删除过期项
				if len(expired) > 0 {
					s.mux.Lock()
					for _, key := range expired {
						if item, ok := s.data.Peek(key); ok && item.IsExpired(ts) {
							s.data.Remove(key)
						}
					}
					s.mux.Unlock()
				}
			}
		}
	}()
}

// Close stops the cache and releases resources | Close 关闭缓存释放资源。
func (s *Cache[K, V]) Close() {
	s.closeOnce.Do(func() { close(s.done) })
	s.Flush(context.Background())
}

// Conn returns the live cached data | Conn 返回缓存数据。
func (s *Cache[K, V]) Conn() map[K]Item[V] {
	s.mux.RLock()
	defer s.mux.RUnlock()

	data := make(map[K]Item[V])
	ts := timer.Timestamp()

	for _, key := range s.data.Keys() {
		if item, ok := s.data.Peek(key); ok && !item.IsExpired(ts) {
			data[key] = *item
		}
	}
	return data
}
