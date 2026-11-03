package authz

import (
	"errors"
	"reflect"
	"time"

	"github.com/casbin/casbin/v3/persist"
	fileadapter "github.com/casbin/casbin/v3/persist/file-adapter"
)

type (
	// Option configures an authorizer before construction | Option 在构造前配置授权器。
	Option interface {
		apply(*Options)
	}

	// OptionFunc adapts a configuration function to Option | OptionFunc 把配置函数适配为 Option。
	OptionFunc func(*Options)

	// Options holds construction configuration and is not safe for concurrent mutation | Options 持有构造配置，并发修改不安全。
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

// DefaultOptions returns an independent default configuration | DefaultOptions 返回独立的默认配置。
func DefaultOptions() *Options {
	return &Options{
		modelFilePath:     "./model.conf",
		policyAdapter:     fileadapter.NewAdapter("./policy.csv"),
		policyLoadTimeout: 5 * time.Second,
	}
}

// NewOptions applies opts to a new default configuration without validating it | NewOptions 将 opts 应用到新的默认配置上，不做校验。
func NewOptions(opts ...Option) *Options {
	options := DefaultOptions()
	options.Apply(opts...)
	return options
}

// Apply applies opts in order without resetting or validating configuration.
// The receiver and options must be valid. Do not mutate the same Options concurrently.
// Apply 按序应用 opts，不重置、不校验配置。
// 接收者与各选项必须已合法；不要并发修改同一个 Options。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate checks final configuration without changing it or creating resources | Validate 检查最终配置，不改动配置、不创建资源。
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
// WithModelFilePath 设置 Casbin 模型文件，路径不能为空。
func WithModelFilePath(path string) Option {
	return OptionFunc(func(o *Options) { o.modelFilePath = path })
}

// WithPolicyAdapter sets policy storage. The adapter must not be nil.
// WithPolicyAdapter 设置策略存储，适配器不能为 nil。
func WithPolicyAdapter(adapter persist.Adapter) Option {
	return OptionFunc(func(o *Options) { o.policyAdapter = adapter })
}

// WithPolicyLoadTimeout bounds GORM policy reads, including the initial load.
// It does not bound migration, waiting for reload locks or Casbin's CPU work.
// WithPolicyLoadTimeout 为 GORM 策略读取设置时限，含首次加载；
// 不约束建表迁移、等待重载锁以及 Casbin 的 CPU 计算。
func WithPolicyLoadTimeout(timeout time.Duration) Option {
	return OptionFunc(func(o *Options) { o.policyLoadTimeout = timeout })
}

// WithWatcher sets an application-owned watcher whose subscription must be ready
// before New. One watcher belongs to one authorizer. New replaces its callback.
// WithWatcher 设置应用持有的 watcher，其订阅必须在 New 之前就绪。
// 一个 watcher 只属于一个授权器；New 会替换它的回调。
func WithWatcher(w persist.Watcher) Option { return OptionFunc(func(o *Options) { o.watcher = w }) }

// WithWatcherErrorHandler receives callback reload errors, outside the reload lock.
// It must be concurrency-safe and must not synchronously reload, notify or close
// the watcher. Schedule recovery separately; logging alone does not repair state.
// WithWatcherErrorHandler 在重载锁之外接收回调里的重载错误。
// 处理函数须并发安全，且不得同步地重载、通知或关闭 watcher；
// 恢复动作要另行调度，仅打日志无法修复状态。
func WithWatcherErrorHandler(h func(error)) Option {
	return OptionFunc(func(o *Options) { o.watcherErrorHandler = h })
}

// WithMaxPolicyAge rejects authorization and role queries at or beyond this
// duration since the start of the last successful load. A successful load taking
// this long is already stale on completion. Zero disables expiry for backwards
// compatibility. Use with an application-managed RunPolicyReload loop.
// WithMaxPolicyAge 设置自上次成功加载起算的时长上限，达到或超过即拒绝
// 授权与角色查询。一次成功加载若耗时正好这么久，完成时即已过期。
// 零值出于兼容不启用过期；应配合应用自管的 RunPolicyReload 循环使用。
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
