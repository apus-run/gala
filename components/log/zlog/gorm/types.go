package gorm

import (
	"time"

	"github.com/apus-run/gala/components/log/zlog"
)

// Config is logger config | 日志配置。
type Config struct {
	// SlowThreshold marks queries slower than this as slow SQL | 超过该时长的查询记为慢 SQL.
	SlowThreshold time.Duration
	// IgnoreRecordNotFoundError silences gorm's ErrRecordNotFound | 为 true 时不记录 gorm 的 ErrRecordNotFound.
	IgnoreRecordNotFoundError bool
	// LogInfo enables per-query info logging | 开启逐条查询的信息日志.
	LogInfo bool
	// LogParams controls whether SQL logs include bound parameter values. Default false:
	// parameters often hold passwords, tokens, phone numbers, and logging them is a leak; enable only for local troubleshooting.
	// LogParams 为 true 时 SQL 日志里带上绑定参数的值。默认 false：参数里常有密码、token、手机号，
	// 进日志就等于泄露；只在本地排查问题时打开。
	LogParams bool

	// Embedded zlog options | 集成 zlog 配置
	*zlog.Options
}

// Option is a config option for the gorm logger | gorm 日志器的配置选项.
type Option interface {
	apply(*Config)
}

// OptionFunc adapts a plain function to Option. | 将普通函数适配为 Option。
type OptionFunc func(*Config)

func (f OptionFunc) apply(o *Config) {
	f(o)
}

// DefaultOptions returns a new Config holding the package defaults. | 返回持有包默认值的新 Config。
func DefaultOptions() *Config {
	return &Config{
		SlowThreshold:             time.Second * 2,
		IgnoreRecordNotFoundError: true,
		LogInfo:                   true,
		Options:                   zlog.DefaultOptions(),
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

// WithSlowThreshold sets the slow SQL threshold | 设置慢 SQL 阈值.
func WithSlowThreshold(threshold time.Duration) Option {
	return OptionFunc(func(config *Config) {
		config.SlowThreshold = threshold
	})
}

// WithIgnoreRecordNotFoundError enables ignoring gorm's ErrRecordNotFoundError | 开启忽略 gorm 的记录不存在错误.
func WithIgnoreRecordNotFoundError() Option {
	return OptionFunc(func(config *Config) {
		config.IgnoreRecordNotFoundError = true
	})
}

// WithLogParams controls whether SQL logs output bound parameter values; off by default | 控制 SQL 日志是否输出绑定参数的值，默认不输出。
func WithLogParams(on bool) Option {
	return OptionFunc(func(config *Config) { config.LogParams = on })
}

// WithLogInfo enables or disables per-query info logging | 开启或关闭逐条查询的信息日志.
func WithLogInfo(logInfo bool) Option {
	return OptionFunc(func(config *Config) {
		config.LogInfo = logInfo
	})
}

// WithConfig sets all config | 设置所有配置。
func WithConfig(fn func(config *Config)) Option {
	return OptionFunc(func(config *Config) {
		fn(config)
	})
}

// WithZlogOptions sets zlog.Options | 设置 zlog.Options。
func WithZlogOptions(options *zlog.Options) Option {
	return OptionFunc(func(conf *Config) {
		conf.Options = options
	})
}
