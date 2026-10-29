package gorm

import (
	"time"

	log "github.com/apus-run/gala/components/log/slog"
)

// Config is logger config
type Config struct {
	SlowThreshold             time.Duration
	IgnoreRecordNotFoundError bool
	LogInfo                   bool
	// LogParams 为 true 时 SQL 日志里带上绑定参数的值。默认 false：参数里常有密码、token、手机号，
	// 进日志就等于泄露；只在本地排查问题时打开。
	LogParams bool

	*log.Options
}

type Option func(*Config)

func DefaultDBConfig() *Config {
	return &Config{
		SlowThreshold:             time.Second * 2,
		IgnoreRecordNotFoundError: true,
		LogInfo:                   true,
		Options:                   log.DefaultOptions(),
	}
}

func Apply(opts ...Option) *Config {
	config := DefaultDBConfig()
	for _, opt := range opts {
		opt(config)
	}
	return config
}

// WithSlowThreshold set slow threshold
func WithSlowThreshold(threshold time.Duration) Option {
	return func(c *Config) {
		c.SlowThreshold = threshold
	}
}

// WithIgnoreRecordNotFoundError ignore record not found error
func WithIgnoreRecordNotFoundError() Option {
	return func(c *Config) {
		c.IgnoreRecordNotFoundError = true
	}
}

// WithLogParams 控制 SQL 日志是否输出绑定参数的值，默认不输出。
func WithLogParams(on bool) Option {
	return func(c *Config) { c.LogParams = on }
}

// WithLogInfo set log info
func WithLogInfo(logInfo bool) Option {
	return func(c *Config) {
		c.LogInfo = logInfo
	}
}

// WithConfig set all config
func WithConfig(fn func(config *Config)) Option {
	return func(config *Config) {
		fn(config)
	}
}

// WithSlogOptions 设置 slog.Options
func WithSlogOptions(options *log.Options) Option {
	return func(conf *Config) {
		conf.Options = options
	}
}
