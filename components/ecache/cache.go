// Package ecache is a generic, thread-safe in-memory cache with TTL, LRC and LRU eviction | ecache 是泛型、并发安全的内存缓存，支持 TTL、LRC 与 LRU 淘汰.
//
// # Notes | 说明
//
// The cache never spawns goroutines. Eviction is lazy: Set drops the oldest
// entry when it is expired, and with MaxKeys set it drops the oldest entry
// regardless of expiry to hold the size. Defaults (TTL 10 years, unlimited
// size) therefore never evict anything automatically — call DeleteExpired
// periodically (about half the TTL) to purge expired entries.
// 缓存不启动任何 goroutine。淘汰是惰性的：Set 在最老条目过期时将其清除；
// 设置 MaxKeys 后超限时无论是否过期都淘汰最老条目以维持容量。默认值（TTL
// 10 年、容量不限）下不会自动清理任何条目——需周期性调用 DeleteExpired
// （约 TTL 的一半）清除过期条目。
//
// Get, Peek, Contains and Values treat expired entries as missing, while Len
// and Keys report everything still held, expired entries included.
// Get、Peek、Contains 与 Values 将过期条目视为不存在；Len 与 Keys 统计仍持有
// 的全部条目，含已过期。
//
// # Features | 功能
//
// - LRC by default, WithLRU switches to recency-based eviction | 默认 LRC，WithLRU 切换为基于最近使用的淘汰
// - Per-entry TTL via Set(key, value, ttl); 0 uses the cache-wide TTL | Set(key, value, ttl) 支持单条目 TTL，0 表示用全局 TTL
// - Stats, OnEvicted callback, Resize, Purge | 命中统计、OnEvicted 回调、Resize、Purge
//
// # Example | 示例
//
//	c := ecache.New[string, *Item](ecache.WithTTL(5*time.Minute), ecache.WithMaxKeys(1000))
//	c.Set("k", v, 0)
//	v, ok := c.Get("k")
package ecache

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// cache provides Cache interface implementation | cache 是 Cache 接口的实现。
type cache[K comparable, V any] struct {
	ttl       time.Duration
	maxKeys   int
	isLRU     bool
	onEvicted func(key K, value V)

	sync.Mutex
	stat      Stats
	items     map[K]*list.Element
	evictList *list.List
}

// New returns a new Cache.
// Default MaxKeys is unlimited (0).
// Default TTL is 10 years, sane value for expirable cache is 5 minutes.
// Default eviction mode is LRC, WithLRU option changes it to LRU.
// New 返回新的 Cache。默认 MaxKeys 不限（0）；默认 TTL 为 10 年，可过期缓存的
// 合理取值约 5 分钟；默认淘汰模式为 LRC，WithLRU 选项可切换为 LRU。
func New[K comparable, V any](opts ...Option) Cache[K, V] {
	options := NewOptions(opts...)

	c := &cache[K, V]{
		items:     map[K]*list.Element{},
		evictList: list.New(),
		ttl:       options.TTL,
		maxKeys:   options.MaxKeys,
		isLRU:     options.IsLRU,
	}
	if options.OnEvicted != nil {
		c.onEvicted = func(key K, value V) { options.OnEvicted(key, value) }
	}
	return c
}

// Add adds a value to the cache. Returns true if an eviction occurred, either
// because the size was exceeded or because the oldest entry expired.
// Returns false if there was no eviction: the item was already in the cache,
// or nothing had to be removed.
// Add 向缓存添加一个值。若因超容量或最老条目过期而发生淘汰则返回 true；
// 未发生淘汰（条目已在缓存中，或无需移除任何条目）则返回 false。
func (c *cache[K, V]) Add(key K, value V) (evicted bool) {
	return c.addWithTTL(key, value, c.ttl)
}

// Set key, ttl of 0 uses the cache-wide TTL | Set 写入键值，ttl 为 0 时使用全局 TTL。
func (c *cache[K, V]) Set(key K, value V, ttl time.Duration) {
	c.addWithTTL(key, value, ttl)
}

// addWithTTL returns true if an eviction occurred, either because the size
// was exceeded or because the oldest entry expired.
// Returns false if there was no eviction: the item was already in the cache,
// or nothing had to be removed.
// addWithTTL 若因超容量或最老条目过期而发生淘汰则返回 true；未发生淘汰（条目
// 已在缓存中，或无需移除任何条目）则返回 false。
func (c *cache[K, V]) addWithTTL(key K, value V, ttl time.Duration) (evicted bool) {
	if ttl == 0 {
		ttl = c.ttl
	}
	now := time.Now()
	c.Lock()
	defer c.Unlock()
	// check for existing item
	// 检查已有条目
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent)
		ent.Value.(*cacheItem[K, V]).value = value
		ent.Value.(*cacheItem[K, V]).expiresAt = now.Add(ttl)
		return false
	}

	// add new item
	// 添加新条目
	ent := &cacheItem[K, V]{key: key, value: value, expiresAt: now.Add(ttl)}
	entry := c.evictList.PushFront(ent)
	c.items[key] = entry
	c.stat.Added++

	// remove the oldest entry if it is expired, only in case of non-default TTL
	// 仅在非默认 TTL 时，若最老条目已过期则将其移除
	if c.ttl != noEvictionTTL || ttl != noEvictionTTL {
		ent := c.evictList.Back()
		if ent != nil && now.After(ent.Value.(*cacheItem[K, V]).expiresAt) {
			c.removeElement(ent)
			evicted = true
		}
	}

	// verify size not exceeded
	// 校验容量未超限
	if c.maxKeys > 0 && len(c.items) > c.maxKeys {
		c.removeOldest()
		evicted = true
	}
	return evicted
}

// Get returns the key value if it's not expired | Get 键未过期时返回其值。
func (c *cache[K, V]) Get(key K) (V, bool) {
	def := *new(V)
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		// expired item check
		// 检查条目是否过期
		if time.Now().After(ent.Value.(*cacheItem[K, V]).expiresAt) {
			c.stat.Misses++
			return ent.Value.(*cacheItem[K, V]).value, false
		}
		if c.isLRU {
			c.evictList.MoveToFront(ent)
		}
		c.stat.Hits++
		return ent.Value.(*cacheItem[K, V]).value, true
	}
	c.stat.Misses++
	return def, false
}

// Contains checks if a key is in the cache, without updating the recent-ness
// or deleting it for being stale. Expired entries are reported as missing.
// Contains 检查键是否在缓存中，不更新最近使用度，也不因过期而删除条目；过期条目视为不存在。
func (c *cache[K, V]) Contains(key K) (ok bool) {
	c.Lock()
	defer c.Unlock()
	ent, ok := c.items[key]
	if !ok {
		return false
	}
	return !time.Now().After(ent.Value.(*cacheItem[K, V]).expiresAt)
}

// Peek returns the key value (or undefined if not found) without updating the "recently used"-ness of the key.
// Works exactly the same as Get in case of LRC mode (default one).
// Peek 返回键值（找不到时为零值），不更新键的"最近使用"状态；在 LRC 模式（默认）下与 Get 行为完全一致。
func (c *cache[K, V]) Peek(key K) (V, bool) {
	def := *new(V)
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		// expired item check
		// 检查条目是否过期
		if time.Now().After(ent.Value.(*cacheItem[K, V]).expiresAt) {
			c.stat.Misses++
			return ent.Value.(*cacheItem[K, V]).value, false
		}
		c.stat.Hits++
		return ent.Value.(*cacheItem[K, V]).value, true
	}
	c.stat.Misses++
	return def, false
}

// GetExpiration returns the expiration time of the key. Non-existing key returns zero time.
// GetExpiration 返回键的过期时间；键不存在时返回零时间。
func (c *cache[K, V]) GetExpiration(key K) (time.Time, bool) {
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		return ent.Value.(*cacheItem[K, V]).expiresAt, true
	}
	return time.Time{}, false
}

// Keys returns a slice of the keys in the cache, from oldest to newest | Keys 返回缓存中的键切片，从最老到最新。
func (c *cache[K, V]) Keys() []K {
	c.Lock()
	defer c.Unlock()
	return c.keys()
}

// Values returns a slice of the values in the cache, from oldest to newest.
// Expired entries are filtered out.
// Values 返回缓存中的值切片，从最老到最新，已过滤过期条目。
func (c *cache[K, V]) Values() []V {
	now := time.Now()
	c.Lock()
	defer c.Unlock()
	values := make([]V, 0, len(c.items))
	for ent := c.evictList.Back(); ent != nil; ent = ent.Prev() {
		if !now.After(ent.Value.(*cacheItem[K, V]).expiresAt) {
			values = append(values, ent.Value.(*cacheItem[K, V]).value)
		}
	}
	return values
}

// Len return count of items in cache, including expired | Len 返回缓存中的条目数（含已过期）。
func (c *cache[K, V]) Len() int {
	c.Lock()
	defer c.Unlock()
	return c.evictList.Len()
}

// Resize changes the cache size. Size of 0 means unlimited | Resize 调整缓存容量，0 表示不限。
func (c *cache[K, V]) Resize(size int) int {
	c.Lock()
	defer c.Unlock()
	if size <= 0 {
		c.maxKeys = 0
		return 0
	}
	diff := max(c.evictList.Len()-size, 0)
	for range diff {
		c.removeOldest()
	}
	c.maxKeys = size
	return diff
}

// Invalidate key (item) from the cache | Invalidate 从缓存中作废指定键（条目）。
func (c *cache[K, V]) Invalidate(key K) {
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		c.removeElement(ent)
	}
}

// InvalidateFn deletes multiple keys if predicate is true | InvalidateFn 删除所有满足谓词的键。
func (c *cache[K, V]) InvalidateFn(fn func(key K) bool) {
	c.Lock()
	defer c.Unlock()
	for key, ent := range c.items {
		if fn(key) {
			c.removeElement(ent)
		}
	}
}

// Remove removes the provided key from the cache, returning if the
// key was contained.
// Remove 从缓存移除给定键，并返回该键是否原本存在。
func (c *cache[K, V]) Remove(key K) bool {
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		c.removeElement(ent)
		return true
	}
	return false
}

// RemoveOldest remove the oldest element in the cache | RemoveOldest 移除并返回缓存中最老的条目。
func (c *cache[K, V]) RemoveOldest() (key K, value V, ok bool) {
	c.Lock()
	defer c.Unlock()
	if ent := c.evictList.Back(); ent != nil {
		c.removeElement(ent)
		return ent.Value.(*cacheItem[K, V]).key, ent.Value.(*cacheItem[K, V]).value, true
	}
	return
}

// GetOldest returns the oldest entry | GetOldest 返回最老的条目。
func (c *cache[K, V]) GetOldest() (key K, value V, ok bool) {
	c.Lock()
	defer c.Unlock()
	if ent := c.evictList.Back(); ent != nil {
		return ent.Value.(*cacheItem[K, V]).key, ent.Value.(*cacheItem[K, V]).value, true
	}
	return
}

// DeleteExpired clears cache of expired items | DeleteExpired 清除缓存中已过期的条目。
func (c *cache[K, V]) DeleteExpired() {
	now := time.Now()
	c.Lock()
	defer c.Unlock()
	var nextEnt *list.Element
	for ent := c.evictList.Back(); ent != nil; ent = nextEnt {
		nextEnt = ent.Prev()
		if now.After(ent.Value.(*cacheItem[K, V]).expiresAt) {
			c.removeElement(ent)
		}
	}
}

// Purge clears the cache completely | Purge 彻底清空缓存。
func (c *cache[K, V]) Purge() {
	c.Lock()
	defer c.Unlock()
	for k, v := range c.items {
		delete(c.items, k)
		c.stat.Evicted++
		if c.onEvicted != nil {
			c.onEvicted(k, v.Value.(*cacheItem[K, V]).value)
		}
	}
	c.evictList.Init()
}

// Stat gets the current stats for cache | Stat 获取缓存当前统计。
func (c *cache[K, V]) Stat() Stats {
	c.Lock()
	defer c.Unlock()
	return c.stat
}

// String returns a human-readable summary of size, stats and hit ratio | String 返回大小、统计与命中率的可读摘要。
func (c *cache[K, V]) String() string {
	stats := c.Stat()
	size := c.Len()
	var ratio float64
	if total := stats.Hits + stats.Misses; total > 0 {
		ratio = 100 * float64(stats.Hits) / float64(total)
	}
	return fmt.Sprintf("Size: %d, Stats: %+v (%0.1f%%)", size, stats, ratio)
}

// keys returns a slice of the keys in the cache, from oldest to newest. Has to be called with lock!
// keys 返回缓存中的键切片，从最老到最新。必须在持锁状态下调用！
func (c *cache[K, V]) keys() []K {
	keys := make([]K, 0, len(c.items))
	for ent := c.evictList.Back(); ent != nil; ent = ent.Prev() {
		keys = append(keys, ent.Value.(*cacheItem[K, V]).key)
	}
	return keys
}

// removeOldest removes the oldest item from the cache. Has to be called with lock!
// removeOldest 移除缓存中最老的条目。必须在持锁状态下调用！
func (c *cache[K, V]) removeOldest() {
	ent := c.evictList.Back()
	if ent != nil {
		c.removeElement(ent)
	}
}

// removeElement is used to remove a given list element from the cache. Has to be called with lock!
// removeElement 从缓存中移除给定的链表元素。必须在持锁状态下调用！
func (c *cache[K, V]) removeElement(e *list.Element) {
	c.evictList.Remove(e)
	kv := e.Value.(*cacheItem[K, V])
	delete(c.items, kv.key)
	c.stat.Evicted++
	if c.onEvicted != nil {
		c.onEvicted(kv.key, kv.value)
	}
}
