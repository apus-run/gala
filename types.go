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

// Task is a background job managed by the Service lifecycle.
//
// Start must return promptly after launching its background loop: long-running
// work belongs in a goroutine owned by the task. Stop must be idempotent and
// bounded by ctx: Run hands each task's Stop a fresh deadline-bounded context
// that carries the application context values but not its cancellation, so an
// externally canceled application context cannot cut cleanup short and one
// task exhausting its budget cannot starve the tasks stopped after it. Stop may
// be called for a task whose Start never ran or did not finish. Nil tasks are
// ignored.
// Task 是受 Service 生命周期管理的后台任务。
//
// Start 必须在启动后台循环后立即返回：长时间运行的工作应放在任务自己持有的
// goroutine 中。Stop 必须幂等，并以其 ctx 约束自身时长：Run 为每个任务的 Stop
// 提供独立的、带截止时间的新鲜 context，它携带应用 context 的值但不继承其取消
// 信号，因此外部取消应用 context 不会截断清理，单个任务耗尽预算也不会饿死其后
// 停止的任务。即使某个任务的 Start 未曾执行或未执行完，也可能收到 Stop 调用。
// nil 任务会被忽略。
type Task interface {
	// Start starts the task | 启动任务.
	Start(ctx context.Context) error
	// Stop stops the task | 停止任务.
	Stop(ctx context.Context) error
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
	// stop timeout for servers and, as a fallback, background tasks | 服务器停止超时，并作为后台任务的回退值
	stopTimeout time.Duration
	// per-task stop timeout | 每个后台任务的停止超时
	taskStopTimeout time.Duration
	// services | 承载服务的服务器列表
	servers []server.Server
	// background tasks managed alongside the servers | 与服务器一同托管的后台任务
	tasks []Task

	context context.Context
	signals []os.Signal

	// Before and After funcs | 启停前后的钩子函数
	beforeStart []func(context.Context) error
	beforeStop  []func(context.Context) error
	afterStart  []func(context.Context) error
	afterStop   []func(context.Context) error
}

// defaultTaskStopTimeout is the stop budget for each background task when
// neither WithTaskStopTimeout nor WithStopTimeout is set.
// defaultTaskStopTimeout 是既未设置 WithTaskStopTimeout 也未设置 WithStopTimeout
// 时每个后台任务的停止预算.
const defaultTaskStopTimeout = 30 * time.Second

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

// effectiveTaskStopTimeout resolves the stop budget applied to each background
// task: taskStopTimeout when set, otherwise stopTimeout when set, otherwise
// defaultTaskStopTimeout.
// effectiveTaskStopTimeout 解析每个后台任务适用的停止预算：优先 taskStopTimeout，
// 其次 stopTimeout，最后 defaultTaskStopTimeout.
func (o *Options) effectiveTaskStopTimeout() time.Duration {
	if o.taskStopTimeout > 0 {
		return o.taskStopTimeout
	}
	if o.stopTimeout > 0 {
		return o.stopTimeout
	}
	return defaultTaskStopTimeout
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

// WithTasks appends background tasks managed by the app lifecycle. Nil entries
// are skipped, so a nil Task never reaches Start or Stop.
// WithTasks 追加由应用生命周期托管的后台任务。nil 条目会被跳过，nil Task 不会
// 进入 Start 或 Stop.
func WithTasks(tasks ...Task) Option {
	return OptionFunc(func(o *Options) {
		for _, task := range tasks {
			if task == nil {
				continue
			}
			o.tasks = append(o.tasks, task)
		}
	})
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

// WithStopTimeout with stop timeout. It bounds server drain and is also the
// fallback stop budget for background tasks; WithTaskStopTimeout overrides it
// for tasks.
// WithStopTimeout 设置停止排水超时。它限定服务器的排水时长，同时作为后台任务的
// 回退停止预算；WithTaskStopTimeout 可为任务单独覆盖.
func WithStopTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.stopTimeout = timeout
	})
}

// WithTaskStopTimeout sets the stop budget applied to each background task. It
// takes precedence over WithStopTimeout for tasks; non-positive values are
// ignored, leaving the fallback in effect.
// WithTaskStopTimeout 设置每个后台任务的停止预算。对任务而言它优先于
// WithStopTimeout；非正值会被忽略，仍沿用回退值.
func WithTaskStopTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) {
		if timeout <= 0 {
			return
		}
		o.taskStopTimeout = timeout
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
