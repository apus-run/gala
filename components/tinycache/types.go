package tinycache

import (
	"context"
	"time"
)

// Cache is the minimal cache contract | Cache 是最小缓存契约.
type Cache interface {
	// Set adds a value to the cache with the default TTL | Set 以默认 TTL 添加值.
	Set(ctx context.Context, key string, value any)

	// SetWithTTL adds a value to the cache with a custom TTL | SetWithTTL 以自定义 TTL 添加值.
	SetWithTTL(ctx context.Context, key string, value any, ttl time.Duration)

	// Get retrieves a value from the cache | Get 从缓存取值.
	Get(ctx context.Context, key string) (any, bool)

	// Delete removes a value from the cache | Delete 从缓存删除值.
	Delete(ctx context.Context, key string)

	// Clear removes all values from the cache | Clear 清空缓存.
	Clear(ctx context.Context)

	// Size returns the number of items in the cache | Size 返回缓存条目数.
	Size() int64

	// Close stops all background tasks and releases resources | Close 停止所有后台任务并释放资源.
	Close() error
}

// item is a cached entry with its expiration and rough size | item 是带过期时间与粗略大小的缓存条目.
type item struct {
	value      any
	expiration time.Time
	size       int // Approximate size in bytes | 字节数近似值
}

// Option is config option | Option 是配置选项.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Options)

// apply runs the adapted function | apply 执行被适配的函数.
func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options holds the cache configuration | Options 保存缓存配置.
type Options struct {
	// DefaultTTL is the default time-to-live for cache entries | 条目默认存活时间.
	DefaultTTL time.Duration

	// CleanupInterval is how often the cache runs cleanup | 清理循环的运行间隔.
	CleanupInterval time.Duration

	// MaxItems is the maximum number of items allowed in the cache | 缓存允许的最大条目数.
	MaxItems int

	// OnEviction is called when an item is evicted from the cache | 条目被淘汰时调用的回调.
	OnEviction func(key string, value any)
}

// DefaultOptions returns an independent default configuration | DefaultOptions 返回一份独立的默认配置.
func DefaultOptions() *Options {
	return &Options{
		DefaultTTL:      10 * time.Minute,
		CleanupInterval: 5 * time.Minute,
		MaxItems:        1000,
		OnEviction:      nil,
	}
}

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
// NewOptions starts from DefaultOptions and applies opts in order.
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
// Apply applies opts in place, in order; it neither resets defaults nor validates.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithDefaultTTL sets the default time-to-live | WithDefaultTTL 设置默认存活时间.
func WithDefaultTTL(ttl time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.DefaultTTL = ttl
	})
}

// WithCleanupInterval sets the cleanup loop interval | WithCleanupInterval 设置清理循环间隔.
func WithCleanupInterval(interval time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.CleanupInterval = interval
	})
}

// WithMaxItems sets the maximum number of items | WithMaxItems 设置最大条目数.
func WithMaxItems(maxItems int) Option {
	return OptionFunc(func(o *Options) {
		o.MaxItems = maxItems
	})
}

// WithOnEviction sets the eviction callback | WithOnEviction 设置淘汰回调.
func WithOnEviction(onEviction func(key string, value any)) Option {
	return OptionFunc(func(o *Options) {
		o.OnEviction = onEviction
	})
}
