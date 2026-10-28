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

// Cache defines a concurrent safe in memory key-value data store.
type Cache struct {
	data       map[string]Item
	done       chan struct{}
	closeOnce  sync.Once
	gcInterval time.Duration
	mux        sync.RWMutex
}

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

// GetAny 与 Get 语义一致：Value 是存入的值本身，不是内部的 Item。
func (s *Cache) GetAny(ctx context.Context, key string) (val cache.Value) {
	val.Value, val.Error = s.Get(ctx, key)
	return
}

func (s *Cache) Set(ctx context.Context, key string, val any, exp time.Duration) error {
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

func (s *Cache) Delete(ctx context.Context, key string) error {
	if len(key) == 0 {
		return errs.ErrKeyNotExist
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	delete(s.data, key)

	return nil
}

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

func (s *Cache) Flush(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.data = make(map[string]Item)

	return nil
}

func (s *Cache) Keys(ctx context.Context) []string {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if len(s.data) == 0 {
		return nil
	}

	ts := timer.Timestamp()
	keys := make([]string, 0, len(s.data))

	for k, v := range s.data {
		// Filter out the expired keys
		if v.Exp == 0 || v.Exp > ts {
			keys = append(keys, k)
		}
	}

	if len(keys) == 0 {
		return nil
	}

	return keys
}

func (s *Cache) Contains(ctx context.Context, key string) bool {
	if len(key) == 0 {
		return false
	}

	s.mux.RLock()
	defer s.mux.RUnlock()

	// 只拿读锁，不能在这里 delete（会与并发读写冲突）；过期项交给 gc 清理。
	v, ok := s.data[key]
	return ok && !v.Expired()
}

// Close stops the garbage collector. It is safe to call more than once.
func (s *Cache) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}

// Conn 返回数据的一份快照。返回内部 map 会让调用方绕过锁读写，构成数据竞争。
func (s *Cache) Conn() map[string]Item {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return maps.Clone(s.data)
}

func (s *Cache) String() string {
	return "memory"
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
				for id, v := range s.data {
					if v.Exp != 0 && v.Exp < ts {
						expired = append(expired, id)
					}
				}
				s.mux.RUnlock()

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
