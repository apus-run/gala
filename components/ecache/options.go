package ecache

import "time"

// noEvictionTTL - very long ttl to prevent eviction | noEvictionTTL——超长 TTL，用于阻止淘汰。
const noEvictionTTL = time.Hour * 24 * 365 * 10

type (
	// OptionFunc adapts a plain function to Option | OptionFunc 将普通函数适配为 Option。
	OptionFunc func(*Options)

	// Option is config option | Option 是配置选项。
	Option interface {
		apply(*Options)
	}

	// Options holds the cache configuration | Options 持有缓存配置。
	Options struct {
		TTL       time.Duration
		MaxKeys   int
		IsLRU     bool
		OnEvicted func(key, value any)
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// DefaultOptions returns a new Options holding the package defaults | DefaultOptions 返回持有包默认值的 Options。
func DefaultOptions() *Options {
	return &Options{
		TTL: noEvictionTTL,
	}
}

// NewOptions builds Options from the defaults, then applies opts in order | NewOptions 从默认值构建 Options，再按序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in order, without resetting defaults or validating | Apply 按顺序应用选项，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithTTL defines TTL for all cache entries.
// By default, it is set to 10 years, sane option for expirable cache might be 5 minutes.
// Non-positive ttl means no eviction, as a zero TTL would expire every entry the moment it is set.
// WithTTL 定义全部缓存条目的 TTL。默认 10 年，可过期缓存的合理取值约 5 分钟；
// 非正 ttl 表示不淘汰（零 TTL 会让每个条目在写入瞬间即过期）。
func WithTTL(ttl time.Duration) Option {
	return OptionFunc(func(o *Options) {
		if ttl <= 0 {
			ttl = noEvictionTTL
		}
		o.TTL = ttl
	})
}

// WithMaxKeys defines how many keys to keep.
// By default, it is 0, which means unlimited.
// WithMaxKeys 定义保留的键数量。默认为 0，表示不限。
func WithMaxKeys(maxKeys int) Option {
	return OptionFunc(func(o *Options) {
		o.MaxKeys = maxKeys
	})
}

// WithLRU sets cache to LRU (Least Recently Used) eviction mode, LRC is the default one | WithLRU 将缓存设为 LRU（最近最少使用）淘汰模式，默认为 LRC。
func WithLRU() Option {
	return OptionFunc(func(o *Options) {
		o.IsLRU = true
	})
}

// WithOnEvicted defines function which would be called automatically for automatically and manually deleted entries.
// key and value are carried as any to keep Option non-generic, so the function must match the cache key and value types.
// WithOnEvicted 定义回调函数，自动淘汰与手动删除的条目都会触发。key 与 value 以 any
// 传递以保持 Option 非泛型，函数签名必须与缓存的键值类型匹配。
func WithOnEvicted[K comparable, V any](fn func(key K, value V)) Option {
	return OptionFunc(func(o *Options) {
		o.OnEvicted = func(key, value any) {
			fn(key.(K), value.(V))
		}
	})
}
