package ecache

import (
	"fmt"
	"time"
)

// Cache defines cache interface | Cache 定义缓存接口。
type Cache[K comparable, V any] interface {
	fmt.Stringer
	// Add adds a value to the cache | Add 向缓存添加值。
	Add(key K, value V) bool
	// Set stores a key, ttl of 0 uses the cache-wide TTL | Set 写入键值，ttl 为 0 时使用全局 TTL。
	Set(key K, value V, ttl time.Duration)
	// Get returns the key value if it's not expired | Get 键未过期时返回其值。
	Get(key K) (V, bool)
	// GetExpiration returns the expiration time of the key | GetExpiration 返回键的过期时间。
	GetExpiration(key K) (time.Time, bool)
	// GetOldest returns the oldest entry | GetOldest 返回最老的条目。
	GetOldest() (K, V, bool)
	// Contains reports whether the key exists and is not expired | Contains 报告键是否存在且未过期。
	Contains(key K) (ok bool)
	// Peek returns the key value without updating recency | Peek 返回键值但不更新最近使用状态。
	Peek(key K) (V, bool)
	// Values returns the values from oldest to newest, expired filtered out | Values 返回从最老到最新的值，已过滤过期条目。
	Values() []V
	// Keys returns the keys from oldest to newest | Keys 返回从最老到最新的键。
	Keys() []K
	// Len returns the item count, expired included | Len 返回条目数（含已过期）。
	Len() int
	// Remove removes the key, reporting whether it was present | Remove 移除键并报告其是否原本存在。
	Remove(key K) bool
	// Invalidate removes the key from the cache | Invalidate 从缓存移除指定键。
	Invalidate(key K)
	// InvalidateFn removes every key matching the predicate | InvalidateFn 移除所有满足谓词的键。
	InvalidateFn(fn func(key K) bool)
	// RemoveOldest removes and returns the oldest entry | RemoveOldest 移除并返回最老的条目。
	RemoveOldest() (K, V, bool)
	// DeleteExpired removes all expired entries | DeleteExpired 清除全部过期条目。
	DeleteExpired()
	// Purge removes all entries | Purge 清空全部条目。
	Purge()
	// Resize changes the capacity, returning the number evicted | Resize 调整容量并返回因此淘汰的条目数。
	Resize(int) int
	// Stat returns the current statistics | Stat 返回当前统计。
	Stat() Stats
}

// Stats provides statistics for cache | Stats 提供缓存统计。
type Stats struct {
	Hits, Misses   int // cache effectiveness | 命中情况
	Added, Evicted int // number of added and evicted records | 新增与淘汰的记录数
}

// cacheItem is used to hold a value in the evictList | cacheItem 用于在 evictList 中持有值。
type cacheItem[K comparable, V any] struct {
	expiresAt time.Time
	key       K
	value     V
}
