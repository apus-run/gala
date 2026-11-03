package gorm

import (
	"time"

	log "github.com/apus-run/gala/components/log/slog"
)

// Config is logger config | 日志配置。
type Config struct {
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
	LogInfo                   bool
	// LogParams controls whether SQL logs include bound parameter values. Default false:
	// parameters often hold passwords, tokens, phone numbers, and logging them is a leak; enable only for local troubleshooting.
	// LogParams 为 true 时 SQL 日志里带上绑定参数的值。默认 false：参数里常有密码、token、手机号，
	// 进日志就等于泄露；只在本地排查问题时打开。
	LogParams bool

	*log.Options
}

type (
	// Option configures the gorm logger Config. | 配置 gorm 日志的 Config。
	Option interface {
		apply(*Config)
	}

	// OptionFunc adapts a plain function to Option. | 将普通函数适配为 Option。
	OptionFunc func(*Config)
)

func (f OptionFunc) apply(o *Config) {
	f(o)
}

// DefaultOptions returns a new Config holding the package defaults. | 返回持有包默认值的新 Config。
func DefaultOptions() *Config {
	return &Config{
		SlowThreshold:             time.Second * 2,
		IgnoreRecordNotFoundError: true,
		LogInfo:                   true,
		Options:                   log.DefaultOptions(),
	}
}

// DefaultDBConfig returns a new Config with the package defaults. | 返回持有包默认值的新 Config。
//
// Deprecated: use DefaultOptions.
func DefaultDBConfig() *Config {
	return DefaultOptions()
}

// NewOptions builds Config from the defaults, then applies opts in order | 从默认值构建 Config，再按顺序应用 opts。
func NewOptions(opts ...Option) *Config {
	c := DefaultOptions()
	c.Apply(opts...)
	return c
}

// Apply applies opts in place, in order; it does not reset defaults or validate | 按顺序原地应用选项，不重置默认值、不做校验。
func (c *Config) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(c)
	}
}

// WithSlowThreshold set slow threshold | 设置慢查询阈值。
func WithSlowThreshold(threshold time.Duration) Option {
	return OptionFunc(func(c *Config) {
		c.SlowThreshold = threshold
	})
}

// WithIgnoreRecordNotFoundError ignore record not found error | 忽略记录不存在错误。
func WithIgnoreRecordNotFoundError() Option {
	return OptionFunc(func(c *Config) {
		c.IgnoreRecordNotFoundError = true
	})
}

// WithLogParams controls whether SQL logs output bound parameter values; off by default | 控制 SQL 日志是否输出绑定参数的值，默认不输出。
func WithLogParams(on bool) Option {
	return OptionFunc(func(c *Config) { c.LogParams = on })
}

// WithLogInfo set log info | 设置日志信息。
func WithLogInfo(logInfo bool) Option {
	return OptionFunc(func(c *Config) {
		c.LogInfo = logInfo
	})
}

// WithConfig set all config | 设置所有配置。
func WithConfig(fn func(config *Config)) Option {
	return OptionFunc(func(config *Config) {
		fn(config)
	})
}

// WithSlogOptions sets slog.Options | 设置 slog.Options。
func WithSlogOptions(options *log.Options) Option {
	return OptionFunc(func(conf *Config) {
		conf.Options = options
	})
}
