// Package tinycache is a minimal process-local TTL cache built on sync.Map | 基于 sync.Map 的极简进程内 TTL 缓存.
//
// # Notes | 说明
//
// TinyCache (the only Cache implementation) stores each item with an
// expiration time; expired items are dropped lazily by Get and periodically
// by a background cleanup loop. The capacity cap counts items only (MaxItems,
// default 1000) — the per-item byte estimate is recorded but never enforced.
// When the cap is exceeded, the items with the earliest expiration are
// evicted first, about 20% at a time; this is not strict LRU. Close must be
// called to stop the cleanup goroutine. Nothing is shared or persisted.
// TinyCache（唯一的 Cache 实现）为每条记录存过期时间；过期条目由 Get 惰性删除，
// 并由后台循环定期清理。容量上限只按条目数（MaxItems，默认 1000）—— 每条估算的
// 字节数只记录、不参与限制。超出上限时优先淘汰过期时间最早的一批（约 20%），
// 不是严格 LRU。需要调用 Close 停止清理协程。不共享、不持久化。
//
// # Features | 功能
//
// - Set / SetWithTTL / Get / Delete / Clear | 设置、取值、删除与清空
// - Defaults: 10 min TTL, 5 min cleanup, 1000 items | 默认 10 分钟 TTL、5 分钟清理、1000 条上限
// - OnEviction callback on expiry and deletion | 过期与删除时触发 OnEviction 回调
// - Options override the defaults | 选项式覆盖默认值
//
// # Example | 示例
//
//	c := tinycache.New(tinycache.WithDefaultTTL(time.Minute))
//	defer c.Close()
//	c.Set(ctx, "session:1", sess)
//	val, ok := c.Get(ctx, "session:1")
package tinycache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// TinyCache is a minimal process-local TTL cache built on sync.Map | TinyCache 是基于 sync.Map 的极简进程内 TTL 缓存.
type TinyCache struct {
	itemCount  atomic.Int64 // Use atomic operations to track item count | 用原子操作维护条目计数
	data       sync.Map
	opts       *Options
	stopChan   chan struct{}
	closedChan chan struct{}
	closeOnce  sync.Once
}

// New creates a cache and starts the background cleanup loop | New 创建缓存并启动后台清理循环.
func New(opts ...Option) *TinyCache {
	options := NewOptions(opts...)
	// NewTicker panics on non-positive values, and that happens in a background goroutine where it cannot be recovered.
	// NewTicker 对非正值会 panic，且发生在后台协程里无法 recover
	if options.CleanupInterval <= 0 {
		options.CleanupInterval = DefaultOptions().CleanupInterval
	}
	c := &TinyCache{
		opts:       options,
		stopChan:   make(chan struct{}),
		closedChan: make(chan struct{}),
	}

	go c.cleanupLoop()
	return c
}

// Set adds a value to the cache with the default TTL | Set 以默认 TTL 向缓存添加值.
func (c *TinyCache) Set(ctx context.Context, key string, value any) {
	c.SetWithTTL(ctx, key, value, c.opts.DefaultTTL)
}

// SetWithTTL adds a value to the cache with a custom TTL | SetWithTTL 以自定义 TTL 向缓存添加值.
func (c *TinyCache) SetWithTTL(_ context.Context, key string, value any, ttl time.Duration) {
	// Estimate size of the item (very rough approximation).
	// 估算条目大小（非常粗略的近似）
	size := estimateSize(value)

	// Check if item already exists to avoid double counting.
	// 检查条目是否已存在，避免重复计数
	if _, exists := c.data.Load(key); exists {
		c.data.Delete(key)
	} else {
		// Only increment if this is a new key.
		// 仅在新 key 时递增
		(&c.itemCount).Add(1)
	}

	c.data.Store(key, item{
		value:      value,
		expiration: time.Now().Add(ttl),
		size:       size,
	})

	// If we're over the max items, clean up old items.
	// 超出最大条目数时清理旧条目
	if c.opts.MaxItems > 0 && (&c.itemCount).Load() > int64(c.opts.MaxItems) {
		c.cleanupOldest()
	}
}

// Get retrieves a value from the cache | Get 从缓存取值.
func (c *TinyCache) Get(_ context.Context, key string) (any, bool) {
	value, ok := c.data.Load(key)
	if !ok {
		return nil, false
	}

	itm, ok := value.(item)
	if !ok {
		// If the value is not of type item, it means it was corrupted or not set correctly.
		// 值不是 item 类型说明数据损坏或未正确写入
		c.data.Delete(key)
		return nil, false
	}
	if time.Now().After(itm.expiration) {
		c.data.Delete(key)
		(&c.itemCount).Add(-1)

		if c.opts.OnEviction != nil {
			c.opts.OnEviction(key, itm.value)
		}

		return nil, false
	}

	return itm.value, true
}

// Delete removes a value from the cache | Delete 从缓存删除值.
func (c *TinyCache) Delete(_ context.Context, key string) {
	if value, loaded := c.data.LoadAndDelete(key); loaded {
		(&c.itemCount).Add(-1)

		if c.opts.OnEviction != nil {
			if itm, ok := value.(item); ok {
				c.opts.OnEviction(key, itm.value)
			}
		}
	}
}

// Clear removes all values from the cache | Clear 清空缓存的所有值.
func (c *TinyCache) Clear(_ context.Context) {
	if c.opts.OnEviction != nil {
		c.data.Range(func(key, value any) bool {
			itm, ok := value.(item)
			if !ok {
				return true
			}
			if keyStr, ok := key.(string); ok {
				c.opts.OnEviction(keyStr, itm.value)
			}
			return true
		})
	}

	// Do not replace the whole map with c.data = sync.Map{}: the background cleanup
	// goroutine reads it concurrently, which would be a data race.
	// 不能用 c.data = sync.Map{} 整体覆盖：后台清理协程在并发读它，那是数据竞争
	c.data.Range(func(key, _ any) bool {
		if _, loaded := c.data.LoadAndDelete(key); loaded {
			c.itemCount.Add(-1)
		}
		return true
	})
}

// Size returns the number of items in the cache | Size 返回缓存条目数.
func (c *TinyCache) Size() int64 {
	return (&c.itemCount).Load()
}

// Close stops the cache cleanup goroutine | Close 停止缓存清理协程.
func (c *TinyCache) Close() error {
	c.closeOnce.Do(func() { close(c.stopChan) })
	<-c.closedChan // Wait for cleanup goroutine to exit | 等待清理协程退出
	return nil
}

// cleanupLoop periodically cleans up expired items | cleanupLoop 定期清理过期条目.
func (c *TinyCache) cleanupLoop() {
	ticker := time.NewTicker(c.opts.CleanupInterval)
	defer func() {
		ticker.Stop()
		close(c.closedChan)
	}()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopChan:
			return
		}
	}
}

// cleanup removes expired items | cleanup 清理过期条目.
func (c *TinyCache) cleanup() {
	evicted := make(map[string]any)
	count := 0

	c.data.Range(func(key, value any) bool {
		itm, ok := value.(item)
		if !ok {
			return true
		}
		if time.Now().After(itm.expiration) {
			c.data.Delete(key)
			count++

			if c.opts.OnEviction != nil {
				if keyStr, ok := key.(string); ok {
					evicted[keyStr] = itm.value
				}
			}
		}
		return true
	})

	if count > 0 {
		(&c.itemCount).Add(-int64(count))

		// Call eviction callbacks outside the loop to avoid blocking the range
		// 在 Range 之外调用淘汰回调，避免阻塞遍历
		if c.opts.OnEviction != nil {
			for k, v := range evicted {
				c.opts.OnEviction(k, v)
			}
		}
	}
}

// cleanupOldest removes the oldest items if we're over the max items | cleanupOldest 超上限时移除最旧的条目.
func (c *TinyCache) cleanupOldest() {
	// Remove 20% of max items at once
	// 一次移除上限的 20%
	threshold := max(c.opts.MaxItems/5, 1)

	currentCount := (&c.itemCount).Load()

	// If we're not over the threshold, don't do anything
	// 未超阈值时不做任何事
	if currentCount <= int64(c.opts.MaxItems) {
		return
	}

	// Find the oldest items
	// 找出最旧的条目
	type keyExpPair struct {
		key        string
		value      any
		expiration time.Time
	}
	candidates := make([]keyExpPair, 0, threshold)

	c.data.Range(func(key, value any) bool {
		itm, ok := value.(item)
		if !ok {
			return true
		}
		if keyStr, ok := key.(string); ok && len(candidates) < threshold {
			candidates = append(candidates, keyExpPair{keyStr, itm.value, itm.expiration})
			return true
		}

		// Find the newest item in candidates
		// 找出候选中最新的条目
		newestIdx := 0
		for i := 1; i < len(candidates); i++ {
			if candidates[i].expiration.After(candidates[newestIdx].expiration) {
				newestIdx = i
			}
		}

		// Replace it if this item is older
		// 该条目更旧时替换之
		if itm.expiration.Before(candidates[newestIdx].expiration) {
			candidates[newestIdx] = keyExpPair{key.(string), itm.value, itm.expiration}
		}

		return true
	})

	// Delete the oldest items
	// 删除最旧的条目
	deletedCount := 0
	for _, candidate := range candidates {
		c.data.Delete(candidate.key)
		deletedCount++

		if c.opts.OnEviction != nil {
			c.opts.OnEviction(candidate.key, candidate.value)
		}
	}

	// Update count
	// 更新计数
	if deletedCount > 0 {
		(&c.itemCount).Add(-int64(deletedCount))
	}
}

// estimateSize attempts to estimate the memory footprint of a value | estimateSize 估算值的内存占用.
func estimateSize(value any) int {
	switch v := value.(type) {
	case string:
		return len(v) + 24 // base size + string overhead | 基础大小 + 字符串开销
	case []byte:
		return len(v) + 24 // base size + slice overhead | 基础大小 + 切片开销
	case map[string]any:
		return len(v) * 64 // rough estimate | 粗略估算
	default:
		return 64 // default conservative estimate | 默认保守估算
	}
}
