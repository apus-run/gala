package gala

import (
	"context"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/apus-run/gala/pkg/ctxkey"
	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/server"
)

var ServiceContextKey = ctxkey.NewContextKey[Gala]()

type Gala interface {
	ID() string
	Name() string
	Version() string
	Metadata() map[string]string
	Endpoint() []string

	Run() error
	Stop() error
}

type Option func(*Options)

type Options struct {
	// service id
	id string
	// service name
	name string
	// service version
	version string
	// service metadata
	metadata map[string]string
	// service endpoints
	endpoints []*url.URL

	// registry
	registry registry.Registry
	// registry timeout
	registryTimeout time.Duration
	// stop timeout
	stopTimeout time.Duration
	// services
	servers []server.Server

	context context.Context
	signals []os.Signal

	// Before and After funcs
	beforeStart []func(context.Context) error
	beforeStop  []func(context.Context) error
	afterStart  []func(context.Context) error
	afterStop   []func(context.Context) error
}

func NewOptions() *Options {
	return &Options{
		context:         context.Background(),
		metadata:        make(map[string]string),
		signals:         []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT},
		registryTimeout: 10 * time.Second,
	}
}

func Apply(opts ...Option) *Options {
	options := NewOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

func WithID(id string) Option {
	return func(o *Options) {
		o.id = id
	}
}

func WithName(name string) Option {
	return func(o *Options) {
		o.name = name
	}
}

func WithVersion(version string) Option {
	return func(o *Options) {
		o.version = version
	}
}

// WithMetadata with service metadata.
func WithMetadata(md map[string]string) Option {
	return func(o *Options) {
		o.metadata = md
	}
}

// WithEndpoint with service endpoint.
func WithEndpoint(endpoints ...*url.URL) Option {
	return func(o *Options) { o.endpoints = endpoints }
}

// WithContext with service context.
func WithContext(ctx context.Context) Option {
	return func(o *Options) { o.context = ctx }
}

// WithServers with transport servers.
func WithServers(srvs ...server.Server) Option {
	return func(o *Options) { o.servers = srvs }
}

// WithSignal with exit signals.
func WithSignal(sigs ...os.Signal) Option {
	return func(o *Options) { o.signals = sigs }
}

// WithRegistry with service registry.
func WithRegistry(r registry.Registry) Option {
	return func(o *Options) {
		o.registry = r
	}
}

// WithRegistryTimeout with registry timeout.
func WithRegistryTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.registryTimeout = timeout
	}
}

// WithStopTimeout with stop timeout.
func WithStopTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.stopTimeout = timeout
	}
}

// Before and Afters

// BeforeStart run func before app starts
func BeforeStart(fn func(context.Context) error) Option {
	return func(o *Options) {
		o.beforeStart = append(o.beforeStart, fn)
	}
}

// BeforeStop run func before app stops
func BeforeStop(fn func(context.Context) error) Option {
	return func(o *Options) {
		o.beforeStop = append(o.beforeStop, fn)
	}
}

// AfterStart run func after app starts
func AfterStart(fn func(context.Context) error) Option {
	return func(o *Options) {
		o.afterStart = append(o.afterStart, fn)
	}
}

// AfterStop run func after app stops
func AfterStop(fn func(context.Context) error) Option {
	return func(o *Options) {
		o.afterStop = append(o.afterStop, fn)
	}
}
