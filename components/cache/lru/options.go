package lru

import (
	"context"
	"time"

	"github.com/hashicorp/golang-lru/v2"

	"github.com/apus-run/gala/components/cache/internal/timer"
)

// Option is config option.
type Option func(*Options)

type Options struct {
	// Context should contain all implementation specific options, using context.WithValue.
	Context context.Context

	Data *lru.Cache[string, *Item]

	// gcInterval 清理过期数据的时间间隔
	GCInterval time.Duration

	Size int
}

// DefaultOptions .
func DefaultOptions() *Options {
	cache, _ := lru.New[string, *Item](1000)

	return &Options{
		Data:       cache,
		GCInterval: 10 * time.Second,
	}
}

func Apply(opts ...Option) *Options {
	options := DefaultOptions()
	for _, o := range opts {
		o(options)
	}
	return options
}

// WithGCInterval 设置后台清理间隔；d<=0 时保持默认值（time.NewTicker 对非正间隔会 panic，
// 而且发生在后台协程里，调用方无法 recover）。
func WithGCInterval(d time.Duration) Option {
	return func(s *Options) {
		if d > 0 {
			s.GCInterval = d
		}
	}
}

// WithSize 设置容量上限；size<1 时忽略。
func WithSize(size int) Option {
	return func(s *Options) {
		if size < 1 {
			return
		}
		s.Size = size
		s.Data.Resize(size)
	}
}

// Data initializes the cache with preconfigured items.
func Data(items map[string]Item) Option {
	return func(o *Options) {
		// lru.New(0) 返回 nil cache 与错误：空 map 也至少给 1 的容量。
		cache, _ := lru.New[string, *Item](max(len(items), 1))
		for k, v := range items {
			cache.Add(k, &v)
		}
		o.Data = cache
	}
}

// WithContext sets the cache context, for any extra configuration.
func WithContext(ctx context.Context) Option {
	return func(o *Options) {
		o.Context = ctx
	}
}

// WithOptions 设置所有配置
func WithOptions(fn func(options *Options)) Option {
	return func(options *Options) {
		fn(options)
	}
}

type Item struct {
	Val any
	Exp int64
}

func NewItem(val any, exp int64) *Item {
	return &Item{Val: val, Exp: exp}
}

// Expired returns true if the item has expired.
func (i Item) Expired() bool {
	return i.Exp != 0 && i.Exp <= timer.Timestamp()
}

func (i Item) IsExpired(currentTS int64) bool {
	return i.Exp != 0 && i.Exp < currentTS
}

func (i Item) Value() any {
	return i.Val
}
