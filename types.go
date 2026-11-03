package gala

import (
	"context"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/server"
)

// Gala is the application contract: identity plus the Run/Stop lifecycle | Gala 是应用契约：身份信息与 Run/Stop 生命周期.
type Gala interface {
	// ID returns the unique instance id | 返回唯一的实例 ID.
	ID() string
	// Name returns the logical service name | 返回逻辑服务名.
	Name() string
	// Version returns the service version | 返回服务版本.
	Version() string
	// Metadata returns arbitrary key/value tags | 返回任意键值标签.
	Metadata() map[string]string
	// Endpoint returns the reachable addresses | 返回可达地址列表.
	Endpoint() []string

	// Run starts the application and blocks until it stops | 启动应用并阻塞至其停止.
	Run() error
	// Stop drains and shuts the application down | 排空并停止应用.
	Stop() error
}

// Option customizes Options | Option 定制 Options.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Options)

// apply runs the adapted function | apply 执行被适配的函数.
func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options holds the application configuration | 持有应用配置.
type Options struct {
	// service id | 服务 id
	id string
	// service name | 服务名
	name string
	// service version | 服务版本
	version string
	// service metadata | 服务元数据
	metadata map[string]string
	// service endpoints | 服务端点
	endpoints []*url.URL

	// registry | 服务注册器
	registry registry.Registry
	// registry timeout | 注册超时
	registryTimeout time.Duration
	// stop timeout | 停止超时
	stopTimeout time.Duration
	// services | 承载服务的服务器列表
	servers []server.Server

	context context.Context
	signals []os.Signal

	// Before and After funcs | 启停前后的钩子函数
	beforeStart []func(context.Context) error
	beforeStop  []func(context.Context) error
	afterStart  []func(context.Context) error
	afterStop   []func(context.Context) error
}

// DefaultOptions returns a new Options holding the package defaults | DefaultOptions 返回持有包默认值的 Options.
func DefaultOptions() *Options {
	return &Options{
		context:         context.Background(),
		metadata:        make(map[string]string),
		signals:         []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT},
		registryTimeout: 10 * time.Second,
	}
}

// NewOptions builds Options from the defaults, then applies opts in order | NewOptions 从默认值出发按顺序应用 opts 构造 Options.
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用选项，不重置默认值、不做校验。
// Apply applies the options in place, in order; it neither resets defaults nor validates.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithID sets the service instance id | WithID 设置服务实例 id.
func WithID(id string) Option {
	return OptionFunc(func(o *Options) {
		o.id = id
	})
}

// WithName sets the service name | WithName 设置服务名.
func WithName(name string) Option {
	return OptionFunc(func(o *Options) {
		o.name = name
	})
}

// WithVersion sets the service version | WithVersion 设置服务版本.
func WithVersion(version string) Option {
	return OptionFunc(func(o *Options) {
		o.version = version
	})
}

// WithMetadata with service metadata | WithMetadata 设置服务元数据.
func WithMetadata(md map[string]string) Option {
	return OptionFunc(func(o *Options) {
		o.metadata = md
	})
}

// WithEndpoint with service endpoint | WithEndpoint 设置服务端点.
func WithEndpoint(endpoints ...*url.URL) Option {
	return OptionFunc(func(o *Options) { o.endpoints = endpoints })
}

// WithContext with service context | WithContext 设置服务的基础 context.
func WithContext(ctx context.Context) Option {
	return OptionFunc(func(o *Options) { o.context = ctx })
}

// WithServers with transport servers | WithServers 设置承载服务的服务器.
func WithServers(srvs ...server.Server) Option {
	return OptionFunc(func(o *Options) { o.servers = srvs })
}

// WithSignal with exit signals | WithSignal 设置触发停止的信号.
func WithSignal(sigs ...os.Signal) Option {
	return OptionFunc(func(o *Options) { o.signals = sigs })
}

// WithRegistry with service registry | WithRegistry 设置服务注册器.
func WithRegistry(r registry.Registry) Option {
	return OptionFunc(func(o *Options) {
		o.registry = r
	})
}

// WithRegistryTimeout with registry timeout | WithRegistryTimeout 设置注册超时.
func WithRegistryTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.registryTimeout = timeout
	})
}

// WithStopTimeout with stop timeout | WithStopTimeout 设置停止排水超时.
func WithStopTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.stopTimeout = timeout
	})
}

// Before and Afters | 启停前后钩子

// BeforeStart run func before app starts | BeforeStart 在应用启动前执行 fn.
func BeforeStart(fn func(context.Context) error) Option {
	return OptionFunc(func(o *Options) {
		o.beforeStart = append(o.beforeStart, fn)
	})
}

// BeforeStop run func before app stops | BeforeStop 在应用停止前执行 fn.
func BeforeStop(fn func(context.Context) error) Option {
	return OptionFunc(func(o *Options) {
		o.beforeStop = append(o.beforeStop, fn)
	})
}

// AfterStart run func after app starts | AfterStart 在应用启动后执行 fn.
func AfterStart(fn func(context.Context) error) Option {
	return OptionFunc(func(o *Options) {
		o.afterStart = append(o.afterStart, fn)
	})
}

// AfterStop run func after app stops | AfterStop 在应用停止后执行 fn.
func AfterStop(fn func(context.Context) error) Option {
	return OptionFunc(func(o *Options) {
		o.afterStop = append(o.afterStop, fn)
	})
}
