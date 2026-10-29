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

type Option func(*Config)

func DefaultDBConfig() *Config {
	return &Config{
		SlowThreshold:             time.Second * 2,
		IgnoreRecordNotFoundError: true,
		LogInfo:                   true,
		Options:                   zlog.DefaultOptions(),
	}
}

func Apply(opts ...Option) *Config {
	config := DefaultDBConfig()
	for _, opt := range opts {
		opt(config)
	}
	return config
}

func WithSlowThreshold(threshold time.Duration) Option {
	return func(config *Config) {
		config.SlowThreshold = threshold
	}
}

func WithIgnoreRecordNotFoundError() Option {
	return func(config *Config) {
		config.IgnoreRecordNotFoundError = true
	}
}

// WithLogParams 控制 SQL 日志是否输出绑定参数的值，默认不输出。
func WithLogParams(on bool) Option {
	return func(config *Config) { config.LogParams = on }
}

func WithLogInfo(logInfo bool) Option {
	return func(config *Config) {
		config.LogInfo = logInfo
	}
}

// WithConfig 设置所有配置
func WithConfig(fn func(config *Config)) Option {
	return func(config *Config) {
		fn(config)
	}
}

// WithZlogOptions 设置 zlog.Options
func WithZlogOptions(options *zlog.Options) Option {
	return func(conf *Config) {
		conf.Options = options
	}
}
