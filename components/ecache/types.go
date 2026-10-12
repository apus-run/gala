package ecache

import (
	"fmt"
	"time"
)

// Cache defines cache interface
type Cache[K comparable, V any] interface {
	fmt.Stringer
	Add(key K, value V) bool
	Set(key K, value V, ttl time.Duration)
	Get(key K) (V, bool)
	GetExpiration(key K) (time.Time, bool)
	GetOldest() (K, V, bool)
	Contains(key K) (ok bool)
	Peek(key K) (V, bool)
	Values() []V
	Keys() []K
	Len() int
	Remove(key K) bool
	Invalidate(key K)
	InvalidateFn(fn func(key K) bool)
	RemoveOldest() (K, V, bool)
	DeleteExpired()
	Purge()
	Resize(int) int
	Stat() Stats
}

// Stats provides statistics for cache
type Stats struct {
	Hits, Misses   int // cache effectiveness
	Added, Evicted int // number of added and evicted records
}

// cacheItem is used to hold a value in the evictList
type cacheItem[K comparable, V any] struct {
	expiresAt time.Time
	key       K
	value     V
}
