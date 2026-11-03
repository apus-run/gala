package layercache

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

const (
	DefaultLocalTTL           = 5 * time.Second
	DefaultRemoteTTL          = 5 * time.Minute
	DefaultNotFoundTTL        = time.Duration(0)
	DefaultLoadTimeout        = 2 * time.Second
	DefaultRemoteTimeout      = 250 * time.Millisecond
	DefaultMaxConcurrentLoads = 128
	DefaultLocalCapacity      = 10_000
)

// Option configures a Cache during construction. Options are applied in order;
// the last setting for a field or local backend wins.
// Option 在构造期配置 Cache。按顺序应用；同字段（或本地后端）以最后设置为准。
type Option interface{ apply(*Options) }

// OptionFunc adapts a synchronous configuration function to Option. It must not
// create external resources, retain the configuration for mutation, or race Apply.
// OptionFunc 把同步配置函数适配为 Option。不得创建外部资源、不得保留配置供
// 之后修改、不得与 Apply 竞争。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) { f(o) }

// Options stores construction settings. Use DefaultOptions or NewOptions;
// its zero value does not contain defaults. Apply only assembles settings,
// Validate checks the final configuration, and New saves an independent value.
// Supplied backends, codecs and callback captures remain application-owned and
// must be concurrency-safe; they are intentionally shared, not deep-copied.
// Options 保存构造期设置。请用 DefaultOptions 或 NewOptions；零值不含默认项。
// Apply 只负责组装，Validate 校验最终配置，New 保存独立副本。传入的后端、
// 编解码器与回调闭包仍归应用所有且必须并发安全；它们是有意共享而非深拷贝。
type Options struct {
	namespace, sourceID, separator string
	local                          Local
	remote                         Remote
	codec                          Codec
	builtinLocal                   bool
	localCapacity                  int

	localTTL, remoteTTL, notFoundTTL, jitter time.Duration
	errNotFound                              error
	loadTimeout, remoteTimeout               time.Duration
	maxConcurrentLoads                       int
	errorHandler                             func(error)

	refreshInterval, refreshIdle        time.Duration
	refreshConcurrency, maxRefreshTasks int
	eventBuffer                         int
	eventHandler                        EventHandler
	statsDisabled                       bool
	statsInterval                       time.Duration
	statsHandler                        StatsHandler
}

// DefaultOptions returns a fresh configuration with a bounded built-in local
// cache enabled. Namespace is required; Remote and maintenance are opt-in.
// DefaultOptions 返回启用内置有界本地缓存的新配置。namespace 必填；
// Remote 与维护功能按需开启。
func DefaultOptions() *Options {
	return &Options{
		separator:          ":",
		codec:              jsonCodec{},
		builtinLocal:       true,
		localCapacity:      DefaultLocalCapacity,
		localTTL:           DefaultLocalTTL,
		remoteTTL:          DefaultRemoteTTL,
		notFoundTTL:        DefaultNotFoundTTL,
		loadTimeout:        DefaultLoadTimeout,
		remoteTimeout:      DefaultRemoteTimeout,
		maxConcurrentLoads: DefaultMaxConcurrentLoads,
		refreshConcurrency: 4,
		maxRefreshTasks:    10_000,
		eventBuffer:        100,
		statsInterval:      time.Minute,
	}
}

// NewOptions applies options over defaults, without validation or allocation of backends.
// NewOptions 在默认值之上应用选项，不校验、不分配后端。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply overwrites settings in order. It does not validate, reset or roll back.
// The receiver and options must be valid and not concurrently modified.
// Apply 按序覆盖设置。不校验、不重置、不回滚。接收者与选项必须合法，
// 且不得并发修改。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate checks the final configuration without modifying it or creating resources.
// Validate 校验最终配置，不修改它、不创建资源。
func (o *Options) Validate() error {
	if strings.TrimSpace(o.namespace) == "" {
		return fmt.Errorf("%w: namespace is required", ErrInvalidOptions)
	}
	if nilLike(o.codec) {
		return fmt.Errorf("%w: codec is required", ErrInvalidOptions)
	}
	if o.builtinLocal && o.localCapacity <= 0 {
		return fmt.Errorf("%w: local capacity must be positive, got %d", ErrInvalidOptions, o.localCapacity)
	}
	if !o.builtinLocal && nilLike(o.local) && nilLike(o.remote) {
		return fmt.Errorf("%w: at least one backend is required", ErrInvalidOptions)
	}
	for _, v := range []struct {
		name      string
		value     time.Duration
		allowZero bool
	}{
		{"local TTL", o.localTTL, false}, {"remote TTL", o.remoteTTL, false}, {"not-found TTL", o.notFoundTTL, true}, {"TTL jitter", o.jitter, true},
	} {
		if v.value < 0 || v.value == 0 && !v.allowZero {
			return fmt.Errorf("%w: %s has invalid value %s", ErrInvalidTTL, v.name, v.value)
		}
	}
	maxTTL := max(o.localTTL, o.remoteTTL, o.notFoundTTL)
	if o.jitter > time.Duration(1<<63-1)-maxTTL {
		return fmt.Errorf("%w: TTL jitter overflows a TTL", ErrInvalidTTL)
	}
	for _, v := range []struct {
		name  string
		value time.Duration
	}{
		{"load timeout", o.loadTimeout}, {"remote timeout", o.remoteTimeout}, {"stats interval", o.statsInterval},
	} {
		if v.value <= 0 {
			return fmt.Errorf("%w: %s must be positive, got %s", ErrInvalidOptions, v.name, v.value)
		}
	}
	for _, v := range []struct {
		name  string
		value int
	}{
		{"max concurrent loads", o.maxConcurrentLoads}, {"refresh concurrency", o.refreshConcurrency}, {"max refresh tasks", o.maxRefreshTasks}, {"event buffer", o.eventBuffer},
	} {
		if v.value <= 0 {
			return fmt.Errorf("%w: %s must be positive, got %d", ErrInvalidOptions, v.name, v.value)
		}
	}
	if o.refreshInterval < 0 || o.refreshInterval > 0 && o.refreshInterval < time.Second || o.refreshInterval > time.Duration(1<<63-1)-time.Second {
		return fmt.Errorf("%w: refresh interval must be zero or at least 1s, with room for its idle timeout", ErrInvalidOptions)
	}
	if o.refreshIdle < 0 {
		return fmt.Errorf("%w: refresh idle timeout must not be negative", ErrInvalidOptions)
	}
	if o.eventHandler != nil && ((!o.builtinLocal && nilLike(o.local)) || nilLike(o.remote)) {
		return fmt.Errorf("%w: event handler requires local and remote backends", ErrInvalidOptions)
	}
	if o.refreshInterval > 0 && !nilLike(o.remote) {
		if _, ok := o.remote.(LeaseRemote); !ok {
			return fmt.Errorf("%w: refresh requires a LeaseRemote backend", ErrUnsupported)
		}
	}
	return nil
}

func nilLike(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// WithNamespace sets the required key domain. Use a stable, nonblank name.
// WithNamespace 设置必填的 key 域，请用稳定、非空的名字。
func WithNamespace(v string) Option { return OptionFunc(func(o *Options) { o.namespace = v }) }

// WithLocal selects an application-owned local store; nil disables the built-in
// local cache. A later WithLocalCapacity selects a fresh built-in store instead.
// WithLocal 选择应用持有的本地存储；nil 关闭内置本地缓存。
// 之后再用 WithLocalCapacity 则改为启用全新的内置存储。
func WithLocal(v Local) Option {
	return OptionFunc(func(o *Options) {
		o.local = v
		o.builtinLocal = false
	})
}

// WithLocalCapacity selects the built-in local cache and its positive entry limit.
// Each New creates its own store; constructing or applying this option does not.
// WithLocalCapacity 选择内置本地缓存及其正数条目上限。每次 New 会创建自己的
// 存储；构造或应用该选项本身不会。
func WithLocalCapacity(v int) Option {
	return OptionFunc(func(o *Options) {
		o.local = nil
		o.builtinLocal = true
		o.localCapacity = v
	})
}

// WithRemote adds an application-owned remote backend; nil leaves local-only caching.
// WithRemote 追加应用持有的远端后端；nil 时仅本地缓存。
func WithRemote(v Remote) Option { return OptionFunc(func(o *Options) { o.remote = v }) }

// WithCodec sets the serializer | WithCodec 设置序列化编解码器。
func WithCodec(v Codec) Option { return OptionFunc(func(o *Options) { o.codec = v }) }

// WithLocalTTL sets the L1 entry lifetime | WithLocalTTL 设置 L1 条目存活时间。
func WithLocalTTL(v time.Duration) Option { return OptionFunc(func(o *Options) { o.localTTL = v }) }

// WithRemoteTTL sets the L2 entry lifetime | WithRemoteTTL 设置 L2 条目存活时间。
func WithRemoteTTL(v time.Duration) Option { return OptionFunc(func(o *Options) { o.remoteTTL = v }) }

// WithNotFoundTTL enables negative caching for a positive TTL; zero disables it.
// WithNotFoundTTL 为正值 TTL 开启空值缓存；零表示关闭。
func WithNotFoundTTL(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.notFoundTTL = v })
}

// WithLoadTimeout bounds one origin load | WithLoadTimeout 限制单次回源加载时长。
func WithLoadTimeout(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.loadTimeout = v })
}

// WithRemoteTimeout bounds one remote operation | WithRemoteTimeout 限制单次远端操作时长。
func WithRemoteTimeout(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.remoteTimeout = v })
}

// WithMaxConcurrentLoads caps shared tasks | WithMaxConcurrentLoads 限制共享任务上限。
func WithMaxConcurrentLoads(v int) Option {
	return OptionFunc(func(o *Options) { o.maxConcurrentLoads = v })
}

// WithErrNotFound recognizes an additional origin sentinel. It does not enable
// negative caching; configure that independently with WithNotFoundTTL.
// WithErrNotFound 追加识别一个回源哨兵错误。它不开启空值缓存；
// 那需要单独用 WithNotFoundTTL 配置。
func WithErrNotFound(v error) Option { return OptionFunc(func(o *Options) { o.errNotFound = v }) }

// WithTTLJitter adds random TTL jitter to dampen synchronized expiry
// | WithTTLJitter 给 TTL 加随机抖动，削弱同时过期的雪崩。
func WithTTLJitter(v time.Duration) Option { return OptionFunc(func(o *Options) { o.jitter = v }) }

// WithErrorHandler observes automatic backfill and maintenance failures. It may
// run concurrently, must return promptly, and must not reenter Cache or Close.
// WithErrorHandler 观察自动回填与维护失败。它可能并发运行，必须快速返回，
// 且不得重入 Cache 或调用 Close。
func WithErrorHandler(v func(error)) Option {
	return OptionFunc(func(o *Options) { o.errorHandler = v })
}

// WithRefreshInterval enables periodic refresh at the given interval
// | WithRefreshInterval 以给定间隔开启周期性刷新。
func WithRefreshInterval(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.refreshInterval = v })
}

// WithRefreshIdleTimeout removes refresh registrations after no refresh-enabled access.
// Zero selects the refresh interval plus one second.
// WithRefreshIdleTimeout 在无刷新启用访问一段时间后移除刷新注册。
// 零表示刷新间隔加一秒。
func WithRefreshIdleTimeout(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.refreshIdle = v })
}

// WithRefreshConcurrency caps simultaneous refresh tasks | WithRefreshConcurrency 限制并发刷新任务数。
func WithRefreshConcurrency(v int) Option {
	return OptionFunc(func(o *Options) { o.refreshConcurrency = v })
}

// WithMaxRefreshTasks caps registered refresh tasks | WithMaxRefreshTasks 限制注册的刷新任务数。
func WithMaxRefreshTasks(v int) Option { return OptionFunc(func(o *Options) { o.maxRefreshTasks = v }) }

// WithSourceID overrides the instance id on events | WithSourceID 覆盖事件上的实例 id。
func WithSourceID(v string) Option { return OptionFunc(func(o *Options) { o.sourceID = v }) }

// WithEventBuffer sets the event channel capacity | WithEventBuffer 设置事件通道容量。
func WithEventBuffer(v int) Option { return OptionFunc(func(o *Options) { o.eventBuffer = v }) }

// WithEventHandler enables best-effort mutation events; nil disables them.
// Both cache levels are required; the application owns event transport.
// WithEventHandler 开启尽力而为的变更事件；nil 关闭。需要两级缓存；
// 事件传输由应用负责。
func WithEventHandler(v EventHandler) Option {
	return OptionFunc(func(o *Options) { o.eventHandler = v })
}

// WithStatsDisabled toggles counter updates | WithStatsDisabled 开关计数器更新。
func WithStatsDisabled(v bool) Option { return OptionFunc(func(o *Options) { o.statsDisabled = v }) }

// WithStatsInterval sets the stats reporting interval | WithStatsInterval 设置统计上报间隔。
func WithStatsInterval(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.statsInterval = v })
}

// WithStatsHandler enables periodic stats reporting | WithStatsHandler 开启周期统计上报。
func WithStatsHandler(v StatsHandler) Option {
	return OptionFunc(func(o *Options) { o.statsHandler = v })
}

// ItemOption configures one Set or Once call. Options are applied in order and
// validated only after the final override, just like construction Options.
// ItemOption 配置单次 Set 或 Once 调用。按序应用，仅在最后一次覆盖之后校验，
// 与构造 Options 相同。
type ItemOption interface{ apply(*ItemOptions) }

// ItemOptionFunc adapts a synchronous operation configuration function. It must
// not retain the configuration, start work, or create external resources.
// ItemOptionFunc 适配同步操作配置函数。不得保留配置、不得启动工作、
// 不得创建外部资源。
type ItemOptionFunc func(*ItemOptions)

func (f ItemOptionFunc) apply(o *ItemOptions) { f(o) }

// ItemOptions has private fields. Cache initializes its TTL from the selected
// backend, applies the options, then validates the final values and capabilities.
// The zero value does not contain those per-cache defaults.
// ItemOptions 字段私有。Cache 先按所选后端初始化 TTL，再应用选项，
// 最后校验最终值与能力。零值不含这些按缓存实例的默认项。
type ItemOptions struct {
	value                      any
	do                         DoFunc
	ttl                        time.Duration
	skipLocal, refresh, nx, xx bool
}

// Apply assembles operation settings in order without validating, resetting or
// rolling back. The receiver and options must be valid and not used concurrently.
// Apply 按序组装操作设置，不校验、不重置、不回滚。接收者与选项必须合法，
// 且不得并发使用。
func (o *ItemOptions) Apply(opts ...ItemOption) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Value is Set's input value or Once's output pointer; nil is valid.
// Value 是 Set 的输入值或 Once 的输出指针；nil 合法。
func Value(v any) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.value = v }) }

// Do supplies the origin function; nil selects cache-only lookup for Once.
// Do 提供回源函数；Once 传 nil 表示仅查缓存。
func Do(v DoFunc) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.do = v }) }

// TTL overrides the positive-entry lifetime for one call; it must be positive.
// TTL 覆盖单次调用的正值条目存活时间；必须为正。
func TTL(v time.Duration) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.ttl = v }) }

// SkipLocal bypasses the L1 read | SkipLocal 跳过 L1 读取。
func SkipLocal(v bool) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.skipLocal = v }) }

// Refresh registers background refresh for the key | Refresh 为该 key 注册后台刷新。
func Refresh(v bool) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.refresh = v }) }

// SetNX enables write-if-absent | SetNX 开启"不存在才写"。
func SetNX(v bool) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.nx = v }) }

// SetXX enables write-if-present | SetXX 开启"存在才写"。
func SetXX(v bool) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.xx = v }) }
func (c *Cache) item(opts []ItemOption) (ItemOptions, error) {
	i := ItemOptions{ttl: c.defaultTTL()}
	for n, opt := range opts {
		if nilLike(opt) {
			return i, fmt.Errorf("%w: item option %d is nil", ErrInvalidOptions, n)
		}
	}
	i.Apply(opts...)
	if i.ttl <= 0 || c.opts.jitter > time.Duration(1<<63-1)-i.ttl {
		return i, ErrInvalidTTL
	}
	if i.skipLocal && c.remote == nil {
		return i, ErrUnsupported
	}
	if i.nx && i.xx {
		return i, ErrInvalidOptions
	}
	if i.refresh && (i.do == nil || c.opts.refreshInterval == 0) {
		return i, ErrInvalidOptions
	}
	if (i.nx || i.xx) && c.remote != nil {
		if _, ok := c.remote.(ConditionalRemote); !ok {
			return i, ErrUnsupported
		}
	}
	return i, nil
}
func (i ItemOptions) mode() SetMode {
	if i.nx {
		return SetIfAbsent
	}
	if i.xx {
		return SetIfPresent
	}
	return SetAlways
}

// WithSeparator sets the T prefix/id separator | WithSeparator 设置 T 的前缀/id 分隔符。
func WithSeparator(v string) Option { return OptionFunc(func(o *Options) { o.separator = v }) }
