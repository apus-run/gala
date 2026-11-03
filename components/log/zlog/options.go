package zlog

import (
	"io"
	"os"

	"go.uber.org/zap/zapcore"
)

// Option is config option. | 配置选项。
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option. | 将普通函数适配为 Option。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options holds the logger configuration | 持有日志器配置.
type Options struct {
	// logger options | 日志配置
	Mode     string              // dev or prod | 开发或生产模式
	Writer   zapcore.WriteSyncer // log output | 日志输出
	LogLevel string              // debug, info, warn, error, panic, panic, fatal
	Format   Format              // text or json | 文本或 JSON
}

// DefaultOptions returns the defaults: dev mode, text format to stdout at info level | 返回默认配置：dev 模式、text 格式、stdout、info 级别。
func DefaultOptions() *Options {
	return &Options{
		Mode:     "dev",
		Writer:   zapcore.AddSync(os.Stdout),
		LogLevel: "info", // zapcore.InfoLevel.String(),
		Format:   FormatText,
	}
}

// NewOptions starts from DefaultOptions and applies opts in order | 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place, in order; it does not reset defaults or validate | 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithMode sets the run mode | 运行模式。
func WithMode(mode string) Option {
	return OptionFunc(func(o *Options) {
		o.Mode = mode
	})
}

// WithLogLevel sets the log level | 设置日志级别。
func WithLogLevel(level string) Option {
	return OptionFunc(func(o *Options) {
		o.LogLevel = level
	})
}

// WithFormat sets the log format | 设置日志格式。
func WithFormat(format Format) Option {
	return OptionFunc(func(o *Options) {
		o.Format = format
	})
}

// WithWriter sets the log output | 设置日志输出。
func WithWriter(writer io.Writer) Option {
	if writer == nil {
		writer = os.Stdout // default to standard output | 默认输出到标准输出
	}
	return OptionFunc(func(o *Options) {
		o.Writer = zapcore.AddSync(writer)
	})
}

// WithWriters sets the log output | 日志输出。
func WithWriters(writers ...io.Writer) Option {
	return OptionFunc(func(o *Options) {
		wrs := make([]zapcore.WriteSyncer, 0, len(writers))
		for _, w := range writers {
			wrs = append(wrs, zapcore.AddSync(w))
		}
		o.Writer = zapcore.NewMultiWriteSyncer(wrs...)
	})
}

// WithOptions sets all options | 设置所有配置。
func WithOptions(fn func(options *Options)) Option {
	return OptionFunc(func(options *Options) {
		fn(options)
	})
}
