package client

import (
	"fmt"
	"time"
)

type (
	// OptionFunc 将配置函数适配为 Option。
	// 函数应同步组装配置，不创建外部资源或保留运行时修改行为。
	OptionFunc func(*Options)

	// Option 表示一个构造配置选项。
	Option interface {
		apply(*Options)
	}

	// Options 保存构造配置，不用于并发热更新。
	// 零值不包含业务默认值，应使用 DefaultOptions 或 NewOptions。
	Options struct {
		timeout time.Duration
		retries int
		labels  map[string]string
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// DefaultOptions 每次创建独立的默认配置。
// 本示例的默认值与业务规则不应直接移植到其它库。
func DefaultOptions() *Options {
	return &Options{
		timeout: 5 * time.Second,
		retries: 3,
		labels:  make(map[string]string),
	}
}

// NewOptions 在默认配置基础上应用选项，不进行校验。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序应用选项，不重置、不校验、不回滚。
// 调用方应保证接收者和选项有效，不与同一配置的读写并发执行。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate 检查最终配置，不修改状态。接收者必须有效。
// 本示例规定 timeout > 0、retries >= 0，实际库沿用自身业务规则。
func (o *Options) Validate() error {
	if o.timeout <= 0 {
		return fmt.Errorf("timeout must be > 0, got %s", o.timeout)
	}
	if o.retries < 0 {
		return fmt.Errorf("retries must be >= 0, got %d", o.retries)
	}
	return nil
}

// WithTimeout 设置超时时间，后应用的同类选项覆盖之前的值。
func WithTimeout(d time.Duration) Option {
	return OptionFunc(func(o *Options) {
		o.timeout = d
	})
}

// WithRetries 设置额外重试次数，0 表示不重试。
func WithRetries(n int) Option {
	return OptionFunc(func(o *Options) {
		o.retries = n
	})
}

// WithLabels 替换全部标签，nil 表示清空。
// 创建选项时获取输入快照，每次应用再复制到独立配置。
// 调用期间，调用方不得并发修改输入 map。
func WithLabels(labels map[string]string) Option {
	snapshot := cloneLabels(labels)
	return OptionFunc(func(o *Options) {
		o.labels = cloneLabels(snapshot)
	})
}

// Timeout 返回超时时间，接收者必须有效。
func (o *Options) Timeout() time.Duration {
	return o.timeout
}

// Retries 返回额外重试次数，接收者必须有效。
func (o *Options) Retries() int {
	return o.retries
}

// Labels 返回标签副本，可能为 nil，接收者必须有效。
func (o *Options) Labels() map[string]string {
	return cloneLabels(o.labels)
}

func (o *Options) clone() Options {
	copied := *o
	copied.labels = cloneLabels(o.labels)
	return copied
}

func cloneLabels(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
