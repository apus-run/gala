package gorm

import (
	"time"

	"github.com/apus-run/gala/components/log/zlog"
)

// Config is logger config
type Config struct {
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
	LogInfo                   bool
	// LogParams 为 true 时 SQL 日志里带上绑定参数的值。默认 false：参数里常有密码、token、手机号，
	// 进日志就等于泄露；只在本地排查问题时打开。
	LogParams bool

	// 集成 zlog 配置
	*zlog.Options
}

type Option interface {
	apply(*Config)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Config)

func (f OptionFunc) apply(o *Config) {
	f(o)
}

// DefaultOptions returns a new Config holding the package defaults.
func DefaultOptions() *Config {
	return &Config{
		SlowThreshold:             time.Second * 2,
		IgnoreRecordNotFoundError: true,
		LogInfo:                   true,
		Options:                   zlog.DefaultOptions(),
	}
}

// DefaultDBConfig returns a new Config with the package defaults.
//
// Deprecated: use DefaultOptions.
func DefaultDBConfig() *Config {
	return DefaultOptions()
}

// NewOptions builds Config from the defaults, then applies opts in order.
func NewOptions(opts ...Option) *Config {
	c := DefaultOptions()
	c.Apply(opts...)
	return c
}

// Apply 按顺序原地应用选项，不重置默认值、不做校验。
func (c *Config) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(c)
	}
}

func WithSlowThreshold(threshold time.Duration) Option {
	return OptionFunc(func(config *Config) {
		config.SlowThreshold = threshold
	})
}

func WithIgnoreRecordNotFoundError() Option {
	return OptionFunc(func(config *Config) {
		config.IgnoreRecordNotFoundError = true
	})
}

// WithLogParams 控制 SQL 日志是否输出绑定参数的值，默认不输出。
func WithLogParams(on bool) Option {
	return OptionFunc(func(config *Config) { config.LogParams = on })
}

func WithLogInfo(logInfo bool) Option {
	return OptionFunc(func(config *Config) {
		config.LogInfo = logInfo
	})
}

// WithConfig 设置所有配置
func WithConfig(fn func(config *Config)) Option {
	return OptionFunc(func(config *Config) {
		fn(config)
	})
}

// WithZlogOptions 设置 zlog.Options
func WithZlogOptions(options *zlog.Options) Option {
	return OptionFunc(func(conf *Config) {
		conf.Options = options
	})
}
