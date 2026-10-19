package authz

import (
	"errors"
	"reflect"
	"time"

	"github.com/casbin/casbin/v3/persist"
	fileadapter "github.com/casbin/casbin/v3/persist/file-adapter"
)

type (
	// Option configures an authorizer before construction.
	Option interface {
		apply(*Options)
	}

	// OptionFunc adapts a configuration function to Option.
	OptionFunc func(*Options)

	// Options holds construction configuration and is not safe for concurrent mutation.
	Options struct {
		modelFilePath       string
		policyAdapter       persist.Adapter
		policyLoadTimeout   time.Duration
		watcher             persist.Watcher
		watcherErrorHandler func(error)
		maxPolicyAge        time.Duration
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// DefaultOptions returns an independent default configuration.
func DefaultOptions() *Options {
	return &Options{
		modelFilePath:     "./model.conf",
		policyAdapter:     fileadapter.NewAdapter("./policy.csv"),
		policyLoadTimeout: 5 * time.Second,
	}
}

// NewOptions applies opts to a new default configuration without validating it.
func NewOptions(opts ...Option) *Options {
	options := DefaultOptions()
	options.Apply(opts...)
	return options
}

// Apply applies opts in order without resetting or validating configuration.
// The receiver and options must be valid. Do not mutate the same Options concurrently.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate checks final configuration without changing it or creating resources.
func (o *Options) Validate() error {
	if o.modelFilePath == "" {
		return errors.New("authz: model file path must be set")
	}
	if isNil(o.policyAdapter) {
		return errors.New("authz: policy adapter must be set")
	}
	if o.policyLoadTimeout <= 0 {
		return errors.New("authz: policy load timeout must be positive")
	}
	if o.maxPolicyAge < 0 {
		return errors.New("authz: max policy age must not be negative")
	}
	if o.watcher != nil && isNil(o.watcher) {
		return errors.New("authz: typed nil watcher")
	}
	if (o.watcher != nil) != (o.watcherErrorHandler != nil) {
		return errors.New("authz: watcher and error handler must be configured together")
	}
	return nil
}

// WithModelFilePath sets the Casbin model file. The path must not be empty.
func WithModelFilePath(path string) Option {
	return OptionFunc(func(o *Options) { o.modelFilePath = path })
}

// WithPolicyAdapter sets policy storage. The adapter must not be nil.
func WithPolicyAdapter(adapter persist.Adapter) Option {
	return OptionFunc(func(o *Options) { o.policyAdapter = adapter })
}

// WithPolicyLoadTimeout bounds GORM policy reads, including the initial load.
// It does not bound migration, waiting for reload locks or Casbin's CPU work.
func WithPolicyLoadTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) { o.policyLoadTimeout = timeout })
}

// WithWatcher sets an application-owned watcher whose subscription must be ready
// before New. One watcher belongs to one authorizer. New replaces its callback.
func WithWatcher(w persist.Watcher) Option { return OptionFunc(func(o *Options) { o.watcher = w }) }

// WithWatcherErrorHandler receives callback reload errors, outside the reload lock.
// It must be concurrency-safe and must not synchronously reload, notify or close
// the watcher. Schedule recovery separately; logging alone does not repair state.
func WithWatcherErrorHandler(h func(error)) Option {
	return OptionFunc(func(o *Options) { o.watcherErrorHandler = h })
}

// WithMaxPolicyAge rejects authorization and role queries at or beyond this
// duration since the start of the last successful load. A successful load taking
// this long is already stale on completion. Zero disables expiry for backwards
// compatibility. Use with an application-managed RunPolicyReload loop.
func WithMaxPolicyAge(age time.Duration) Option {
	return OptionFunc(func(o *Options) { o.maxPolicyAge = age })
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}
