package tinycache

import (
	"context"
	"time"
)

type Cache interface {
	// Set adds a value to the cache with the default TTL.
	Set(ctx context.Context, key string, value any)

	// SetWithTTL adds a value to the cache with a custom TTL.
	SetWithTTL(ctx context.Context, key string, value any, ttl time.Duration)

	// Get retrieves a value from the cache.
	Get(ctx context.Context, key string) (any, bool)

	// Delete removes a value from the cache.
	Delete(ctx context.Context, key string)

	// Clear removes all values from the cache.
	Clear(ctx context.Context)

	// Size returns the number of items in the cache.
	Size() int64

	// Close stops all background tasks and releases resources.
	Close() error
}

type item struct {
	value      any
	expiration time.Time
	size       int // Approximate size in bytes
}

// Option is config option.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

type Options struct {
	// DefaultTTL is the default time-to-live for cache entries.
	DefaultTTL time.Duration

	// CleanupInterval is how often the cache runs cleanup.
	CleanupInterval time.Duration

	// MaxItems is the maximum number of items allowed in the cache.
	MaxItems int

	// OnEviction is called when an item is evicted from the cache.
	OnEviction func(key string, value any)
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{
		DefaultTTL:      10 * time.Minute,
		CleanupInterval: 5 * time.Minute,
		MaxItems:        1000,
		OnEviction:      nil,
	}
}

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

func WithDefaultTTL(ttl time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.DefaultTTL = ttl
	})
}

func WithCleanupInterval(interval time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.CleanupInterval = interval
	})
}

func WithMaxItems(maxItems int) Option {
	return OptionFunc(func(o *Options) {
		o.MaxItems = maxItems
	})
}

func WithOnEviction(onEviction func(key string, value any)) Option {
	return OptionFunc(func(o *Options) {
		o.OnEviction = onEviction
	})
}
