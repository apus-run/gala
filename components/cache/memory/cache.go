// Package memory provides a concurrent-safe in-memory cache with TTL, backed
// by a plain map | memory 提供并发安全、带 TTL 的内存缓存，底层是普通 map.
//
// # Notes | 说明
//
// Cache implements [cache.Cache]: string keys, any values, second-granular
// TTL, a background GC goroutine that Close must stop. The map is unbounded
// (memory has no WithSize), so bound the key set yourself. The v2 subpackage
// is the generic counterpart with typed keys and values.
// Cache 实现 [cache.Cache]：string 键、any 值、秒级 TTL、由 Close 停止的后台
// gc 协程。底层 map 无上限（memory 没有 WithSize），需自行约束 key 集合规模。
// v2 子包是键值带类型的泛型版本。
//
// # Example | 示例
//
//	c := memory.New(memory.WithGCInterval(time.Second))
//	_ = c.Set(ctx, "k", "v", time.Minute)
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

var (
	_ cache.Cache = (*Cache)(nil)
)

// Cache defines a concurrent-safe in-memory key-value store with expiry |
// Cache 是并发安全、带过期机制的内存 KV 缓存。
type Cache struct {
	data       map[string]Item
	done       chan struct{}
	closeOnce  sync.Once
	gcInterval time.Duration
	mux        sync.RWMutex
}

// New returns a memory cache and starts the background GC goroutine; Close must
// be called once the cache is no longer used, or the goroutine stays alive
// forever.
// New 返回一个内存缓存并启动后台 gc 协程；不再使用时必须 Close，否则协程一直存活。
func New(opts ...Option) *Cache {
	options := NewOptions(opts...)

	s := &Cache{
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
func (s *Cache) Get(ctx context.Context, key string) (any, error) {
	if len(key) == 0 {
		return nil, errs.ErrKeyNotExist
	}
	s.mux.RLock()
	defer s.mux.RUnlock()

	item, ok := s.data[key]
	if !ok {
		return nil, errs.ErrKeyNotExist
	}
	if item.Expired() {
		return nil, errs.ErrItemExpired
	}

	return item.Value(), nil
}

// GetAny matches Get: Value is the stored value itself, not the internal Item.
// GetAny 与 Get 语义一致：Value 是存入的值本身，不是内部的 Item。
func (s *Cache) GetAny(ctx context.Context, key string) (val cache.Value) {
	val.Value, val.Error = s.Get(ctx, key)
	return
}

// Set stores a value with an expiry | Set 写入缓存（带过期时间）。
func (s *Cache) Set(ctx context.Context, key string, val any, exp time.Duration) error {
	// An empty key or nil value cannot be written: fail loudly instead of
	// pretending success, or callers would believe the write landed.
	// 空 key 或 nil 值写不进去：报错而不是假装成功，否则调用方以为写入了。
	if len(key) == 0 || val == nil {
		return errs.ErrSetKeyFailed
	}

	e := timer.ExpireAt(exp)

	s.mux.Lock()
	defer s.mux.Unlock()

	item := NewItem(val, e)
	s.data[key] = *item

	return nil
}

// Delete removes the given key | Delete 删除指定键。
func (s *Cache) Delete(ctx context.Context, key string) error {
	if len(key) == 0 {
		return errs.ErrKeyNotExist
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	delete(s.data, key)

	return nil
}

// Deletes removes keys in batch, returning how many were removed | Deletes 批量删除，
// 返回实际删除个数。
func (s *Cache) Deletes(ctx context.Context, keys ...string) (int64, error) {
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

// Flush clears the whole cache | Flush 清空缓存。
func (s *Cache) Flush(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.data = make(map[string]Item)

	return nil
}

// Keys lists the valid (unexpired) keys | Keys 返回有效键列表。
func (s *Cache) Keys(ctx context.Context) []string {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if len(s.data) == 0 {
		return nil
	}

	ts := timer.Timestamp()
	keys := make([]string, 0, len(s.data))

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
func (s *Cache) Contains(ctx context.Context, key string) bool {
	if len(key) == 0 {
		return false
	}

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
func (s *Cache) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}

// Conn returns a snapshot of the data. Handing out the internal map would let
// callers read and write around the lock, a data race.
// Conn 返回数据的一份快照。返回内部 map 会让调用方绕过锁读写，构成数据竞争。
func (s *Cache) Conn() map[string]Item {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return maps.Clone(s.data)
}

// String describes the cache | String 缓存状态描述。
func (s *Cache) String() string {
	return "memory"
}

func (s *Cache) gc() {
	go func() {
		ticker := time.NewTicker(s.gcInterval)
		defer ticker.Stop()
		// pre-allocate the slice
		// 内存预分配
		expired := make([]string, 0, 100)

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
