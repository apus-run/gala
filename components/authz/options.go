package authz

import (
	"errors"
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
		modelFilePath     string
		policyAdapter     persist.Adapter
		policyLoadTimeout time.Duration
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
	if o.policyAdapter == nil {
		return errors.New("authz: policy adapter must be set")
	}
	if o.policyLoadTimeout <= 0 {
		return errors.New("authz: policy load timeout must be positive")
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
