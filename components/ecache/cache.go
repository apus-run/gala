// Package ecache implements Cache similar to hashicorp/golang-lru
//
// Support LRC, LRU and TTL-based eviction.
// Package is thread-safe and doesn't spawn any goroutines.
// On every Set() call, cache deletes single oldest entry in case it's expired.
// In case MaxSize is set, cache deletes the oldest entry disregarding its expiration date to maintain the size,
// either using LRC or LRU eviction.
// In case of default TTL (10 years) and default MaxSize (0, unlimited) the cache will be truly unlimited
// and will never delete entries from itself automatically.
//
// Get, Peek, Contains and Values treat an expired entry as missing, while Len and Keys report
// everything the cache still holds, expired entries included.
//
// Important: only reliable way of not having expired entries stuck in a cache is to
// run cache.DeleteExpired periodically using time.Ticker, advisable period is 1/2 of TTL.
package ecache

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// cache provides Cache interface implementation.
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
func New[K comparable, V any](opts ...Option) Cache[K, V] {
	options := Apply(opts...)

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
func (c *cache[K, V]) Add(key K, value V) (evicted bool) {
	return c.addWithTTL(key, value, c.ttl)
}

// Set key, ttl of 0 would use cache-wide TTL
func (c *cache[K, V]) Set(key K, value V, ttl time.Duration) {
	c.addWithTTL(key, value, ttl)
}

// Returns true if an eviction occurred, either because the size was exceeded
// or because the oldest entry expired.
// Returns false if there was no eviction: the item was already in the cache,
// or nothing had to be removed.
func (c *cache[K, V]) addWithTTL(key K, value V, ttl time.Duration) (evicted bool) {
	if ttl == 0 {
		ttl = c.ttl
	}
	now := time.Now()
	c.Lock()
	defer c.Unlock()
	// Check for existing item
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent)
		ent.Value.(*cacheItem[K, V]).value = value
		ent.Value.(*cacheItem[K, V]).expiresAt = now.Add(ttl)
		return false
	}

	// Add new item
	ent := &cacheItem[K, V]{key: key, value: value, expiresAt: now.Add(ttl)}
	entry := c.evictList.PushFront(ent)
	c.items[key] = entry
	c.stat.Added++

	// Remove the oldest entry if it is expired, only in case of non-default TTL.
	if c.ttl != noEvictionTTL || ttl != noEvictionTTL {
		ent := c.evictList.Back()
		if ent != nil && now.After(ent.Value.(*cacheItem[K, V]).expiresAt) {
			c.removeElement(ent)
			evicted = true
		}
	}

	// Verify size not exceeded
	if c.maxKeys > 0 && len(c.items) > c.maxKeys {
		c.removeOldest()
		evicted = true
	}
	return evicted
}

// Get returns the key value if it's not expired
func (c *cache[K, V]) Get(key K) (V, bool) {
	def := *new(V)
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		// Expired item check
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
func (c *cache[K, V]) Peek(key K) (V, bool) {
	def := *new(V)
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		// Expired item check
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
func (c *cache[K, V]) GetExpiration(key K) (time.Time, bool) {
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		return ent.Value.(*cacheItem[K, V]).expiresAt, true
	}
	return time.Time{}, false
}

// Keys returns a slice of the keys in the cache, from oldest to newest.
func (c *cache[K, V]) Keys() []K {
	c.Lock()
	defer c.Unlock()
	return c.keys()
}

// Values returns a slice of the values in the cache, from oldest to newest.
// Expired entries are filtered out.
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

// Len return count of items in cache, including expired
func (c *cache[K, V]) Len() int {
	c.Lock()
	defer c.Unlock()
	return c.evictList.Len()
}

// Resize changes the cache size. Size of 0 means unlimited.
func (c *cache[K, V]) Resize(size int) int {
	c.Lock()
	defer c.Unlock()
	if size <= 0 {
		c.maxKeys = 0
		return 0
	}
	diff := c.evictList.Len() - size
	if diff < 0 {
		diff = 0
	}
	for i := 0; i < diff; i++ {
		c.removeOldest()
	}
	c.maxKeys = size
	return diff
}

// Invalidate key (item) from the cache
func (c *cache[K, V]) Invalidate(key K) {
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		c.removeElement(ent)
	}
}

// InvalidateFn deletes multiple keys if predicate is true
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
func (c *cache[K, V]) Remove(key K) bool {
	c.Lock()
	defer c.Unlock()
	if ent, ok := c.items[key]; ok {
		c.removeElement(ent)
		return true
	}
	return false
}

// RemoveOldest remove the oldest element in the cache
func (c *cache[K, V]) RemoveOldest() (key K, value V, ok bool) {
	c.Lock()
	defer c.Unlock()
	if ent := c.evictList.Back(); ent != nil {
		c.removeElement(ent)
		return ent.Value.(*cacheItem[K, V]).key, ent.Value.(*cacheItem[K, V]).value, true
	}
	return
}

// GetOldest returns the oldest entry
func (c *cache[K, V]) GetOldest() (key K, value V, ok bool) {
	c.Lock()
	defer c.Unlock()
	if ent := c.evictList.Back(); ent != nil {
		return ent.Value.(*cacheItem[K, V]).key, ent.Value.(*cacheItem[K, V]).value, true
	}
	return
}

// DeleteExpired clears cache of expired items
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

// Purge clears the cache completely.
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

// Stat gets the current stats for cache
func (c *cache[K, V]) Stat() Stats {
	c.Lock()
	defer c.Unlock()
	return c.stat
}

func (c *cache[K, V]) String() string {
	stats := c.Stat()
	size := c.Len()
	var ratio float64
	if total := stats.Hits + stats.Misses; total > 0 {
		ratio = 100 * float64(stats.Hits) / float64(total)
	}
	return fmt.Sprintf("Size: %d, Stats: %+v (%0.1f%%)", size, stats, ratio)
}

// Keys returns a slice of the keys in the cache, from oldest to newest. Has to be called with lock!
func (c *cache[K, V]) keys() []K {
	keys := make([]K, 0, len(c.items))
	for ent := c.evictList.Back(); ent != nil; ent = ent.Prev() {
		keys = append(keys, ent.Value.(*cacheItem[K, V]).key)
	}
	return keys
}

// removeOldest removes the oldest item from the cache. Has to be called with lock!
func (c *cache[K, V]) removeOldest() {
	ent := c.evictList.Back()
	if ent != nil {
		c.removeElement(ent)
	}
}

// removeElement is used to remove a given list element from the cache. Has to be called with lock!
func (c *cache[K, V]) removeElement(e *list.Element) {
	c.evictList.Remove(e)
	kv := e.Value.(*cacheItem[K, V])
	delete(c.items, kv.key)
	c.stat.Evicted++
	if c.onEvicted != nil {
		c.onEvicted(kv.key, kv.value)
	}
}
