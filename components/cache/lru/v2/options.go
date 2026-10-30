package lru

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2"

	"github.com/apus-run/gala/components/cache/internal/timer"
)

// Option 泛型配置项
type Option[K comparable, V any] interface {
	apply(*Options[K, V])
}

// OptionFunc adapts a plain function to Option.
type OptionFunc[K comparable, V any] func(*Options[K, V])

func (f OptionFunc[K, V]) apply(o *Options[K, V]) {
	f(o)
}

type Options[K comparable, V any] struct {
	Context    context.Context
	Data       *lru.Cache[K, *Item[V]]
	GCInterval time.Duration
	Size       int
}

// DefaultOptions 默认泛型配置
func DefaultOptions[K comparable, V any]() *Options[K, V] {
	cache, _ := lru.New[K, *Item[V]](1000)

	return &Options[K, V]{
		Data:       cache,
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

// WithGCInterval 泛型版本
// WithGCInterval 设置后台清理间隔；d<=0 时保持默认值（time.NewTicker 对非正间隔会 panic，
// 而且发生在后台协程里，调用方无法 recover）。
func WithGCInterval[K comparable, V any](d time.Duration) Option[K, V] {
	return OptionFunc[K, V](func(s *Options[K, V]) {
		if d > 0 {
			s.GCInterval = d
		}
	})
}

// WithSize 泛型版本
// size<1 时忽略。
func WithSize[K comparable, V any](size int) Option[K, V] {
	return OptionFunc[K, V](func(s *Options[K, V]) {
		if size < 1 {
			return
		}
		s.Size = size
		s.Data.Resize(size)
	})
}

// Data 泛型数据初始化
func Data[K comparable, V any](items map[K]Item[V]) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		// lru.New(0) 返回 nil cache 与错误：空 map 也至少给 1 的容量。
		cache, _ := lru.New[K, *Item[V]](max(len(items), 1))
		for k, v := range items {
			cache.Add(k, &v)
		}
		o.Data = cache
	})
}

// WithContext 保持原样
func WithContext[K comparable, V any](ctx context.Context) Option[K, V] {
	return OptionFunc[K, V](func(o *Options[K, V]) {
		o.Context = ctx
	})
}

// WithOptions 泛型版本
func WithOptions[K comparable, V any](fn func(options *Options[K, V])) Option[K, V] {
	return OptionFunc[K, V](func(options *Options[K, V]) {
		fn(options)
	})
}

// Item 泛型存储项
type Item[V any] struct {
	Val V
	Exp int64
}

func NewItem[V any](val V, exp int64) *Item[V] {
	return &Item[V]{Val: val, Exp: exp}
}

// Expired 保持逻辑不变
func (i Item[V]) Expired() bool {
	return i.Exp != 0 && i.Exp <= timer.Timestamp()
}

func (i Item[V]) IsExpired(currentTS int64) bool {
	return i.Exp != 0 && i.Exp < currentTS
}

func (i Item[V]) Value() V {
	return i.Val
}
