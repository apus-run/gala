package memory

import (
	"context"
	"maps"
	"time"

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

// Options holds the generic in-memory cache configuration | Options 持有泛型内存缓存的配置.
type Options[K comparable, V any] struct {
	// Context should contain all implementation specific options, using
	// context.WithValue | Context 携带实现特定的配置，用 context.WithValue 塞入。
	Context context.Context

	// Data is the underlying storage map | Data 是底层存储 map。
	Data map[K]Item[V]

	// GCInterval is the interval for cleaning expired data | 清理过期数据的时间间隔。
	GCInterval time.Duration
}

// DefaultOptions returns the default generic options | DefaultOptions 返回默认泛型配置。
func DefaultOptions[K comparable, V any]() *Options[K, V] {
	return &Options[K, V]{
		Data:       make(map[K]Item[V]),
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
// WithGCInterval 设置后台清理间隔；d<=0 时保持默认值（time.NewTicker 对非正间隔会 panic，
// 而且发生在后台协程里，调用方无法 recover）。
func WithGCInterval[K comparable, V any](d time.Duration) Option[K, V] {
	return OptionFunc[K, V](func(s *Options[K, V]) {
		if d > 0 {
			s.GCInterval = d
		}
	})
}

// Data initializes the cache with preconfigured items.
//
// The items map is copied: holding the caller's map directly would let later
// caller mutations bypass the cache's lock. nil is treated as empty.
// Data 用预置数据初始化缓存（泛型版本）。
//
// 复制 items：直接持有调用方的 map，调用方事后改它就等于绕过缓存的锁。nil 视为空。
func Data[K comparable, V any](items map[K]Item[V]) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		o.Data = make(map[K]Item[V], len(items))
		maps.Copy(o.Data, items)
	})
}

// WithContext sets the cache context, for any extra configuration |
// WithContext 设置缓存 context，用于携带额外配置。
func WithContext[K comparable, V any](ctx context.Context) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		o.Context = ctx
	})
}

// WithOptions sets all options at once | WithOptions 设置所有配置。
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

// Expired reports whether the item has expired | Expired 表示条目是否已过期。
func (i *Item[V]) Expired() bool {
	return i.Exp != 0 && i.Exp <= timer.Timestamp()
}

// Value returns the stored value | Value 返回存入的值。
func (i *Item[V]) Value() V {
	return i.Val
}
