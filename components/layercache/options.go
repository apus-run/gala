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
type Option interface{ apply(*Options) }

// OptionFunc adapts a synchronous configuration function to Option. It must not
// create external resources, retain the configuration for mutation, or race Apply.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) { f(o) }

// Options stores construction settings. Use DefaultOptions or NewOptions;
// its zero value does not contain defaults. Apply only assembles settings,
// Validate checks the final configuration, and New saves an independent value.
// Supplied backends, codecs and callback captures remain application-owned and
// must be concurrency-safe; they are intentionally shared, not deep-copied.
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
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply overwrites settings in order. It does not validate, reset or roll back.
// The receiver and options must be valid and not concurrently modified.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate checks the final configuration without modifying it or creating resources.
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
func WithNamespace(v string) Option { return OptionFunc(func(o *Options) { o.namespace = v }) }

// WithLocal selects an application-owned local store; nil disables the built-in
// local cache. A later WithLocalCapacity selects a fresh built-in store instead.
func WithLocal(v Local) Option {
	return OptionFunc(func(o *Options) {
		o.local = v
		o.builtinLocal = false
	})
}

// WithLocalCapacity selects the built-in local cache and its positive entry limit.
// Each New creates its own store; constructing or applying this option does not.
func WithLocalCapacity(v int) Option {
	return OptionFunc(func(o *Options) {
		o.local = nil
		o.builtinLocal = true
		o.localCapacity = v
	})
}

// WithRemote adds an application-owned remote backend; nil leaves local-only caching.
func WithRemote(v Remote) Option           { return OptionFunc(func(o *Options) { o.remote = v }) }
func WithCodec(v Codec) Option             { return OptionFunc(func(o *Options) { o.codec = v }) }
func WithLocalTTL(v time.Duration) Option  { return OptionFunc(func(o *Options) { o.localTTL = v }) }
func WithRemoteTTL(v time.Duration) Option { return OptionFunc(func(o *Options) { o.remoteTTL = v }) }

// WithNotFoundTTL enables negative caching for a positive TTL; zero disables it.
func WithNotFoundTTL(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.notFoundTTL = v })
}
func WithLoadTimeout(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.loadTimeout = v })
}
func WithRemoteTimeout(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.remoteTimeout = v })
}
func WithMaxConcurrentLoads(v int) Option {
	return OptionFunc(func(o *Options) { o.maxConcurrentLoads = v })
}

// WithErrNotFound recognizes an additional origin sentinel. It does not enable
// negative caching; configure that independently with WithNotFoundTTL.
func WithErrNotFound(v error) Option       { return OptionFunc(func(o *Options) { o.errNotFound = v }) }
func WithTTLJitter(v time.Duration) Option { return OptionFunc(func(o *Options) { o.jitter = v }) }

// WithErrorHandler observes automatic backfill and maintenance failures. It may
// run concurrently, must return promptly, and must not reenter Cache or Close.
func WithErrorHandler(v func(error)) Option {
	return OptionFunc(func(o *Options) { o.errorHandler = v })
}
func WithRefreshInterval(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.refreshInterval = v })
}

// WithRefreshIdleTimeout removes refresh registrations after no refresh-enabled access.
// Zero selects the refresh interval plus one second.
func WithRefreshIdleTimeout(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.refreshIdle = v })
}
func WithRefreshConcurrency(v int) Option {
	return OptionFunc(func(o *Options) { o.refreshConcurrency = v })
}
func WithMaxRefreshTasks(v int) Option { return OptionFunc(func(o *Options) { o.maxRefreshTasks = v }) }
func WithSourceID(v string) Option     { return OptionFunc(func(o *Options) { o.sourceID = v }) }
func WithEventBuffer(v int) Option     { return OptionFunc(func(o *Options) { o.eventBuffer = v }) }

// WithEventHandler enables best-effort mutation events; nil disables them.
// Both cache levels are required; the application owns event transport.
func WithEventHandler(v EventHandler) Option {
	return OptionFunc(func(o *Options) { o.eventHandler = v })
}
func WithStatsDisabled(v bool) Option { return OptionFunc(func(o *Options) { o.statsDisabled = v }) }
func WithStatsInterval(v time.Duration) Option {
	return OptionFunc(func(o *Options) { o.statsInterval = v })
}
func WithStatsHandler(v StatsHandler) Option {
	return OptionFunc(func(o *Options) { o.statsHandler = v })
}

// ItemOption configures one Set or Once call. Options are applied in order and
// validated only after the final override, just like construction Options.
type ItemOption interface{ apply(*ItemOptions) }

// ItemOptionFunc adapts a synchronous operation configuration function. It must
// not retain the configuration, start work, or create external resources.
type ItemOptionFunc func(*ItemOptions)

func (f ItemOptionFunc) apply(o *ItemOptions) { f(o) }

// ItemOptions has private fields. Cache initializes its TTL from the selected
// backend, applies the options, then validates the final values and capabilities.
// The zero value does not contain those per-cache defaults.
type ItemOptions struct {
	value                      any
	do                         DoFunc
	ttl                        time.Duration
	skipLocal, refresh, nx, xx bool
}

// Apply assembles operation settings in order without validating, resetting or
// rolling back. The receiver and options must be valid and not used concurrently.
func (o *ItemOptions) Apply(opts ...ItemOption) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Value is Set's input value or Once's output pointer; nil is valid.
func Value(v any) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.value = v }) }

// Do supplies the origin function; nil selects cache-only lookup for Once.
func Do(v DoFunc) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.do = v }) }

// TTL overrides the positive-entry lifetime for one call; it must be positive.
func TTL(v time.Duration) ItemOption { return ItemOptionFunc(func(i *ItemOptions) { i.ttl = v }) }
func SkipLocal(v bool) ItemOption    { return ItemOptionFunc(func(i *ItemOptions) { i.skipLocal = v }) }
func Refresh(v bool) ItemOption      { return ItemOptionFunc(func(i *ItemOptions) { i.refresh = v }) }
func SetNX(v bool) ItemOption        { return ItemOptionFunc(func(i *ItemOptions) { i.nx = v }) }
func SetXX(v bool) ItemOption        { return ItemOptionFunc(func(i *ItemOptions) { i.xx = v }) }
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

func WithSeparator(v string) Option { return OptionFunc(func(o *Options) { o.separator = v }) }
