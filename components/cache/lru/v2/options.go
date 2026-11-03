package lru

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2"

	"github.com/apus-run/gala/components/cache/internal/timer"
)

// Option is a generic config option | Option 泛型配置项。
type Option[K comparable, V any] interface {
	apply(*Options[K, V])
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配成 Option。
type OptionFunc[K comparable, V any] func(*Options[K, V])

func (f OptionFunc[K, V]) apply(o *Options[K, V]) {
	f(o)
}

// Options holds the generic LRU cache configuration | Options 持有泛型 LRU 缓存的配置.
type Options[K comparable, V any] struct {
	// Context carries implementation specific options via context.WithValue |
	// Context 携带实现特定的配置，用 context.WithValue 塞入。
	Context context.Context
	// Data is the underlying LRU store | Data 是底层 LRU 存储。
	Data *lru.Cache[K, *Item[V]]
	// GCInterval is the interval for cleaning expired data | 清理过期数据的时间间隔。
	GCInterval time.Duration
	// Size is the capacity limit | 容量上限。
	Size int
}

// DefaultOptions returns the default generic options | DefaultOptions 默认泛型配置。
func DefaultOptions[K comparable, V any]() *Options[K, V] {
	cache, _ := lru.New[K, *Item[V]](1000)

	return &Options[K, V]{
		Data:       cache,
		GCInterval: 10 * time.Second,
	}
}

// NewOptions builds Options from the defaults, then applies opts in order |
// NewOptions 从默认配置出发，按顺序应用 opts。
func NewOptions[K comparable, V any](opts ...Option[K, V]) *Options[K, V] {
	o := DefaultOptions[K, V]()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place, in order, without resetting defaults or
// validating | Apply 按顺序原地应用选项，不重置默认值、不做校验。
func (o *Options[K, V]) Apply(opts ...Option[K, V]) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithGCInterval sets the background cleanup interval; d<=0 keeps the default
// (time.NewTicker panics on a non-positive interval, inside the background
// goroutine, where callers cannot recover it).
// WithGCInterval 设置后台清理间隔（泛型版本）；d<=0 时保持默认值（time.NewTicker
// 对非正间隔会 panic，而且发生在后台协程里，调用方无法 recover）。
func WithGCInterval[K comparable, V any](d time.Duration) Option[K, V] {
	return OptionFunc[K, V](func(s *Options[K, V]) {
		if d > 0 {
			s.GCInterval = d
		}
	})
}

// WithSize sets the capacity limit; size<1 is ignored | WithSize 设置容量上限
// （泛型版本）；size<1 时忽略。
func WithSize[K comparable, V any](size int) Option[K, V] {
	return OptionFunc[K, V](func(s *Options[K, V]) {
		if size < 1 {
			return
		}
		s.Size = size
		s.Data.Resize(size)
	})
}

// Data initializes the cache with preconfigured items | Data 用预置数据初始化缓存
// （泛型版本）。
func Data[K comparable, V any](items map[K]Item[V]) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		// lru.New(0) returns a nil cache and an error: even an empty map gets a
		// capacity of at least 1.
		// lru.New(0) 返回 nil cache 与错误：空 map 也至少给 1 的容量。
		cache, _ := lru.New[K, *Item[V]](max(len(items), 1))
		for k, v := range items {
			cache.Add(k, &v)
		}
		o.Data = cache
	})
}

// WithContext sets the cache context, same as the non-generic version |
// WithContext 设置缓存 context（与非泛型版保持原样）。
func WithContext[K comparable, V any](ctx context.Context) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		o.Context = ctx
	})
}

// WithOptions sets all options at once | WithOptions 设置所有配置（泛型版本）。
func WithOptions[K comparable, V any](fn func(options *Options[K, V])) Option[K, V] {
	return OptionFunc[K, V](func(options *Options[K, V]) {
		fn(options)
	})
}

// Item is a generic cache entry | Item 泛型存储项。
type Item[V any] struct {
	// Val is the stored value | 存入的值。
	Val V
	// Exp is the unix-second expiry timestamp; 0 means never expires |
	// 过期时间戳（unix 秒），0 表示永不过期。
	Exp int64
}

// NewItem creates an Item from a value and an expiry timestamp |
// NewItem 用值和过期时间戳构造 Item。
func NewItem[V any](val V, exp int64) *Item[V] {
	return &Item[V]{Val: val, Exp: exp}
}

// Expired reports whether the item has expired; the logic is unchanged from
// the non-generic version | Expired 表示条目是否已过期（逻辑保持不变）。
func (i Item[V]) Expired() bool {
	return i.Exp != 0 && i.Exp <= timer.Timestamp()
}

// IsExpired reports expiry against the given timestamp | IsExpired 按给定时间戳判断是否过期。
func (i Item[V]) IsExpired(currentTS int64) bool {
	return i.Exp != 0 && i.Exp < currentTS
}

// Value returns the stored value | Value 返回存入的值。
func (i Item[V]) Value() V {
	return i.Val
}
