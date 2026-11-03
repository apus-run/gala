package memory

import (
	"context"
	"maps"
	"time"

	"github.com/apus-run/gala/components/cache/internal/timer"
)

// Option is a config option | Option 是配置选项。
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配成 Option。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options holds the in-memory cache configuration | Options 持有内存缓存的配置.
type Options struct {
	// Context should contain all implementation specific options, using
	// context.WithValue | Context 携带实现特定的配置，用 context.WithValue 塞入。
	Context context.Context

	// Data is the underlying storage map | Data 是底层存储 map。
	Data map[string]Item

	// GCInterval is the interval for cleaning expired data | 清理过期数据的时间间隔。
	GCInterval time.Duration
}

// DefaultOptions returns the default options | DefaultOptions 返回默认配置。
func DefaultOptions() *Options {
	return &Options{
		Data:       make(map[string]Item),
		GCInterval: 10 * time.Second,
	}
}

// NewOptions starts from DefaultOptions and applies opts in order |
// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place, in order, without resetting defaults or
// validating | Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithGCInterval sets the background cleanup interval; d<=0 keeps the default
// (time.NewTicker panics on a non-positive interval, inside the background
// goroutine, where callers cannot recover it).
// WithGCInterval 设置后台清理间隔；d<=0 时保持默认值（time.NewTicker 对非正间隔会 panic，
// 而且发生在后台协程里，调用方无法 recover）。
func WithGCInterval(d time.Duration) Option {
	return OptionFunc(func(s *Options) {
		if d > 0 {
			s.GCInterval = d
		}
	})
}

// Data initializes the cache with preconfigured items.
//
// The items map is copied: holding the caller's map directly would let later
// caller mutations bypass the cache's lock. nil is treated as empty.
// Data 用预置数据初始化缓存。
//
// 复制 items：直接持有调用方的 map，调用方事后改它就等于绕过缓存的锁。nil 视为空。
func Data(items map[string]Item) Option {
	return OptionFunc(func(o *Options) {
		o.Data = make(map[string]Item, len(items))
		maps.Copy(o.Data, items)
	})
}

// WithContext sets the cache context, for any extra configuration |
// WithContext 设置缓存 context，用于携带额外配置。
func WithContext(ctx context.Context) Option {
	return OptionFunc(func(o *Options) {
		o.Context = ctx
	})
}

// WithOptions sets all options at once | WithOptions 设置所有配置。
func WithOptions(fn func(options *Options)) Option {
	return OptionFunc(func(options *Options) {
		fn(options)
	})
}

// Item is one cache entry | Item 是一条缓存项。
type Item struct {
	// Val is the stored value | 存入的值。
	Val any
	// Exp is the unix-second expiry timestamp; 0 means never expires |
	// 过期时间戳（unix 秒），0 表示永不过期。
	Exp int64
}

// NewItem creates an Item from a value and an expiry timestamp |
// NewItem 用值和过期时间戳构造 Item。
func NewItem(val any, exp int64) *Item {
	return &Item{Val: val, Exp: exp}
}

// Expired returns true if the item has expired | Expired 表示条目是否已过期。
func (i Item) Expired() bool {
	return i.Exp != 0 && i.Exp <= timer.Timestamp()
}

// Value returns the stored value | Value 返回存入的值。
func (i Item) Value() any {
	return i.Val
}
