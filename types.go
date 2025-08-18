package gala

import (
	"context"
	"net/url"
	"os"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/server"
)

type Gala interface {
	ID() string
	Name() string
	Version() string
	Metadata() map[string]string
	Endpoint() string

	Run() error
	Stop() error
}

type Option func(*Options)

type Options struct {
	// ID is the unique identifier for the service instance.
	id string
	// Name is the name of the service.
	name string
	// Version is the version of the service.
	version string
	// Metadata is the metadata associated with the service.
	metadata map[string]string

	// server endpoints
	endpoints []*url.URL

	ctx context.Context

	registry registry.Registry
	servers  []server.Server

	context context.Context
	signals []os.Signal

	// Before and After func
	beforeStart []func(context.Context) error
	beforeStop  []func(context.Context) error
	afterStart  []func(context.Context) error
	afterStop   []func(context.Context) error
}

func NewOptions() *Options {
	return &Options{
		metadata: make(map[string]string),
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

// WithServer with transport servers.
func WithServer(srv ...server.Server) Option {
	return func(o *Options) { o.servers = srv }
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
