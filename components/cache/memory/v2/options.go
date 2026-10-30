package memory

import (
	"context"
	"maps"
	"time"

	"github.com/apus-run/gala/components/cache/internal/timer"
)

// Option is config option.
type Option[K comparable, V any] interface {
	apply(*Options[K, V])
}

// OptionFunc adapts a plain function to Option.
type OptionFunc[K comparable, V any] func(*Options[K, V])

func (f OptionFunc[K, V]) apply(o *Options[K, V]) {
	f(o)
}

type Options[K comparable, V any] struct {
	// Context should contain all implementation specific options, using context.WithValue.
	Context context.Context

	Data map[K]Item[V]

	// gcInterval 清理过期数据的时间间隔
	GCInterval time.Duration
}

// DefaultOptions .
func DefaultOptions[K comparable, V any]() *Options[K, V] {
	return &Options[K, V]{
		Data:       make(map[K]Item[V]),
		GCInterval: 10 * time.Second,
	}
}

// NewOptions builds Options from the defaults, then applies opts in order.
func NewOptions[K comparable, V any](opts ...Option[K, V]) *Options[K, V] {
	o := DefaultOptions[K, V]()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用选项，不重置默认值、不做校验。
func (o *Options[K, V]) Apply(opts ...Option[K, V]) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

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
// 复制 items：直接持有调用方的 map，调用方事后改它就等于绕过缓存的锁。nil 视为空。
func Data[K comparable, V any](items map[K]Item[V]) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		o.Data = make(map[K]Item[V], len(items))
		maps.Copy(o.Data, items)
	})
}

// WithContext sets the cache context, for any extra configuration.
func WithContext[K comparable, V any](ctx context.Context) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		o.Context = ctx
	})
}

// WithOptions 设置所有配置
func WithOptions[K comparable, V any](fn func(options *Options[K, V])) Option[K, V] {
	return OptionFunc[K, V](func(options *Options[K, V]) {
		fn(options)
	})
}

type Item[V any] struct {
	Val V
	Exp int64
}

func NewItem[V any](val V, exp int64) *Item[V] {
	return &Item[V]{Val: val, Exp: exp}
}

func (i *Item[V]) Expired() bool {
	return i.Exp != 0 && i.Exp <= timer.Timestamp()
}

func (i *Item[V]) Value() V {
	return i.Val
}
