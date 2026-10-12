package ecache

import "time"

// noEvictionTTL - very long ttl to prevent eviction
const noEvictionTTL = time.Hour * 24 * 365 * 10

// Option is config option.
type Option func(*Options)

type Options struct {
	TTL       time.Duration
	MaxKeys   int
	IsLRU     bool
	OnEvicted func(key, value any)
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{
		TTL: noEvictionTTL,
	}
}

func Apply(opts ...Option) *Options {
	options := DefaultOptions()
	for _, o := range opts {
		o(options)
	}
	return options
}

// WithTTL defines TTL for all cache entries.
// By default, it is set to 10 years, sane option for expirable cache might be 5 minutes.
// Non-positive ttl means no eviction, as a zero TTL would expire every entry the moment it is set.
func WithTTL(ttl time.Duration) Option {
	return func(o *Options) {
		if ttl <= 0 {
			ttl = noEvictionTTL
		}
		o.TTL = ttl
	}
}

// WithMaxKeys defines how many keys to keep.
// By default, it is 0, which means unlimited.
func WithMaxKeys(maxKeys int) Option {
	return func(o *Options) {
		o.MaxKeys = maxKeys
	}
}

// WithLRU sets cache to LRU (Least Recently Used) eviction mode, LRC is the default one.
func WithLRU() Option {
	return func(o *Options) {
		o.IsLRU = true
	}
}

// WithOnEvicted defines function which would be called automatically for automatically and manually deleted entries.
// key and value are carried as any to keep Option non-generic, so the function must match the cache key and value types.
func WithOnEvicted[K comparable, V any](fn func(key K, value V)) Option {
	return func(o *Options) {
		o.OnEvicted = func(key, value any) {
			fn(key.(K), value.(V))
		}
	}
}
