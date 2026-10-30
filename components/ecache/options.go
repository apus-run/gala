package ecache

import "time"

// noEvictionTTL - very long ttl to prevent eviction
const noEvictionTTL = time.Hour * 24 * 365 * 10

type (
	// OptionFunc adapts a plain function to Option.
	OptionFunc func(*Options)

	// Option is config option.
	Option interface {
		apply(*Options)
	}

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

// DefaultOptions returns a new Options holding the package defaults.
func DefaultOptions() *Options {
	return &Options{
		TTL: noEvictionTTL,
	}
}

// NewOptions builds Options from the defaults, then applies opts in order.
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序应用选项，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithTTL defines TTL for all cache entries.
// By default, it is set to 10 years, sane option for expirable cache might be 5 minutes.
// Non-positive ttl means no eviction, as a zero TTL would expire every entry the moment it is set.
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
func WithMaxKeys(maxKeys int) Option {
	return OptionFunc(func(o *Options) {
		o.MaxKeys = maxKeys
	})
}

// WithLRU sets cache to LRU (Least Recently Used) eviction mode, LRC is the default one.
func WithLRU() Option {
	return OptionFunc(func(o *Options) {
		o.IsLRU = true
	})
}

// WithOnEvicted defines function which would be called automatically for automatically and manually deleted entries.
// key and value are carried as any to keep Option non-generic, so the function must match the cache key and value types.
func WithOnEvicted[K comparable, V any](fn func(key K, value V)) Option {
	return OptionFunc(func(o *Options) {
		o.OnEvicted = func(key, value any) {
			fn(key.(K), value.(V))
		}
	})
}
