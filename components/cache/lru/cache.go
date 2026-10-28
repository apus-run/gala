package lru

import (
	"context"
	"sync"
	"time"

	"github.com/apus-run/gala/components/cache"
	"github.com/apus-run/gala/components/cache/internal/errs"
	"github.com/apus-run/gala/components/cache/internal/timer"

	"github.com/hashicorp/golang-lru/v2"
)

var (
	_ cache.Cache = (*Cache)(nil)
)

// Cache 接口的LRU缓存
type Cache struct {
	data       *lru.Cache[string, *Item]
	mux        sync.RWMutex
	gcInterval time.Duration // 后台清理间隔
	done       chan struct{} // 停止信号
	closeOnce  sync.Once
}

// New 创建带过期机制的LRU缓存并启动后台 gc 协程；不再使用时必须 Close，否则协程一直存活。
func New(opts ...Option) *Cache {
	options := Apply(opts...)

	s := &Cache{
		gcInterval: options.GCInterval,
		data:       options.Data,
		done:       make(chan struct{}),
	}

	// Start garbage collector
	timer.StartTimeStampUpdater()
	s.gc()

	return s
}

// Set 设置缓存（带过期时间）
func (s *Cache) Set(ctx context.Context, key string, val any, exp time.Duration) error {
	// 空 key 或 nil 值写不进去：报错而不是假装成功，否则调用方以为写入了。
	if len(key) == 0 || val == nil {
		return errs.ErrSetKeyFailed
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	e := timer.ExpireAt(exp)

	item := &Item{
		Val: val,
		Exp: e,
	}

	s.data.Add(key, item)
	return nil
}

// Get 获取缓存（带过期检查）
func (s *Cache) Get(ctx context.Context, key string) (any, error) {
	s.mux.RLock()
	item, ok := s.data.Get(key)
	s.mux.RUnlock()

	if !ok {
		return nil, errs.ErrKeyNotExist
	}

	// 过期项留给后台 gc 删除：在这里异步 Delete 会删掉期间重新写入的新值。
	if item.Expired() {
		return nil, errs.ErrItemExpired
	}
	return item.Value(), nil
}

// GetAny 获取封装后的值，错误与 Get 一致。
// 不在这里加锁：Get 自己会拿读锁，读锁重入在有写者排队时会死锁。
func (s *Cache) GetAny(ctx context.Context, key string) (val cache.Value) {
	val.Value, val.Error = s.Get(ctx, key)
	return
}

// Delete 删除指定键
func (s *Cache) Delete(ctx context.Context, key string) error {
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

// Deletes 批量删除
func (s *Cache) Deletes(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	var n int64
	for _, key := range keys {
		if s.data.Remove(key) {
			n++
		}
	}
	return n, nil
}

// Flush 清空缓存
func (s *Cache) Flush(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.data.Purge()
	return nil
}

// Keys 获取有效键列表
func (s *Cache) Keys(ctx context.Context) []string {
	s.mux.RLock()
	defer s.mux.RUnlock()

	keys := s.data.Keys()
	var result []string // 没有有效 key 时返回 nil，与 memory、redis 一致
	for _, k := range keys {
		if item, ok := s.data.Peek(k); ok && !item.Expired() {
			result = append(result, k)
		}
	}
	return result
}

// Contains 检查键是否存在且有效
func (s *Cache) Contains(ctx context.Context, key string) bool {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if item, ok := s.data.Peek(key); ok {
		return !item.Expired()
	}
	return false
}

// String 缓存状态描述
func (s *Cache) String() string {
	return "lru"
}

func (s *Cache) gc() {
	go func() {
		ticker := time.NewTicker(s.gcInterval)
		defer ticker.Stop()
		// 内存预分配
		expired := make([]string, 0, 100)

		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				ts := timer.Timestamp()
				expired = expired[:0]

				// 锁定以读取数据
				s.mux.RLock()
				keys := s.data.Keys()
				for _, key := range keys {
					if item, ok := s.data.Peek(key); ok && item.IsExpired(ts) {
						expired = append(expired, key)
					}
				}
				s.mux.RUnlock()

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

// Close 关闭缓存释放资源
func (s *Cache) Close() {
	s.closeOnce.Do(func() { close(s.done) })
	s.Flush(context.Background())
}

// Conn 返回缓存数据
func (s *Cache) Conn() map[string]Item {
	s.mux.RLock()
	defer s.mux.RUnlock()

	data := make(map[string]Item)
	ts := timer.Timestamp()

	for _, key := range s.data.Keys() {
		if item, ok := s.data.Peek(key); ok && !item.IsExpired(ts) {
			data[key] = *item
		}
	}
	return data
}
