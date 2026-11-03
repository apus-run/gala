package slog

import (
	"io"
	"log/slog"
	"os"
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
	LogLevel string      // debug, info, warn, error,debug, panic
	Format   Format      // text or json | 文本或 JSON
	Writer   io.Writer   // log output | 日志输出
	LogGroup string      // slog group | slog 分组
	LogAttrs []slog.Attr // log attributes | 日志属性
}

// DefaultOptions returns the defaults: text format to stdout at info level | 返回默认配置：text 格式、stdout、info 级别。
func DefaultOptions() *Options {
	return &Options{
		LogLevel: "info",
		Format:   FormatText,
		Writer:   os.Stdout,
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

// WithLogLevel sets the log level | 设置日志级别。
func WithLogLevel(level string) Option {
	return OptionFunc(func(o *Options) {
		o.LogLevel = level
	})
}

// WithLogGroup sets the log group | 设置日志分组。
func WithLogGroup(group string) Option {
	return OptionFunc(func(o *Options) {
		o.LogGroup = group
	})
}

// WithLogAttrs sets log attributes | 设置日志属性。
func WithLogAttrs(attrs []Attr) Option {
	return OptionFunc(func(o *Options) {
		o.LogAttrs = attrs
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
		writer = os.Stdout
	}
	return OptionFunc(func(o *Options) {
		o.Writer = writer
	})
}

// WithOptions sets all options | 设置所有配置。
func WithOptions(fn func(options *Options)) Option {
	return OptionFunc(func(options *Options) {
		fn(options)
	})
}
