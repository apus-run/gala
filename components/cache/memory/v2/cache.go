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

// Cache defines a concurrent safe in memory key-value data store.
type Cache[K comparable, V any] struct {
	data       map[K]Item[V]
	done       chan struct{}
	closeOnce  sync.Once
	gcInterval time.Duration
	mux        sync.RWMutex
}

func New[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	options := Apply(opts...)

	s := &Cache[K, V]{
		gcInterval: options.GCInterval,
		data:       options.Data,
		done:       make(chan struct{}),
	}

	// Start garbage collector
	timer.StartTimeStampUpdater()
	s.gc()

	return s
}

func (s *Cache[K, V]) Get(ctx context.Context, key K) (V, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	var zero V
	item, ok := s.data[key]
	if !ok {
		return zero, errs.ErrKeyNotExist
	}
	// 过期时返回零值，不把已过期的旧值交给调用方。
	if item.Expired() {
		return zero, errs.ErrItemExpired
	}

	return item.Value(), nil
}

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

func (s *Cache[K, V]) Set(ctx context.Context, key K, val V, exp time.Duration) error {
	e := timer.ExpireAt(exp)

	s.mux.Lock()
	defer s.mux.Unlock()

	item := NewItem(val, e)
	s.data[key] = *item

	return nil
}

func (s *Cache[K, V]) Delete(ctx context.Context, key K) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	delete(s.data, key)

	return nil
}

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

func (s *Cache[K, V]) Len(ctx context.Context) int {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return len(s.data)
}

func (s *Cache[K, V]) Flush(ctx context.Context) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.data = make(map[K]Item[V])

	return nil
}

func (s *Cache[K, V]) Keys(ctx context.Context) []K {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if len(s.data) == 0 {
		return nil
	}

	ts := timer.Timestamp()
	keys := make([]K, 0, len(s.data))

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

func (s *Cache[K, V]) Contains(ctx context.Context, key K) bool {
	s.mux.RLock()
	defer s.mux.RUnlock()

	// 只拿读锁，不能在这里 delete（会与并发读写冲突）；过期项交给 gc 清理。
	v, ok := s.data[key]
	return ok && !v.Expired()
}

// Close stops the garbage collector. It is safe to call more than once.
func (s *Cache[K, V]) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	return nil
}

// Conn 返回数据的一份快照。返回内部 map 会让调用方绕过锁读写，构成数据竞争。
func (s *Cache[K, V]) Conn() map[K]Item[V] {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return maps.Clone(s.data)
}

func (s *Cache[K, V]) String() string {
	return "memory"
}

func (s *Cache[K, V]) gc() {
	go func() {
		ticker := time.NewTicker(s.gcInterval)
		defer ticker.Stop()
		// 内存预分配
		expired := make([]K, 0, 100)

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
