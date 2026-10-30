package slog

import (
	"io"
	"log/slog"
	"os"
)

// Option is config option.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

type Options struct {
	// logger options
	LogLevel string      // debug, info, warn, error,debug, panic
	Format   Format      // text or json
	Writer   io.Writer   // 日志输出
	LogGroup string      // slog group
	LogAttrs []slog.Attr // 日志属性
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{
		LogLevel: "info",
		Format:   FormatText,
		Writer:   os.Stdout,
	}
}

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithLogLevel 日志级别
func WithLogLevel(level string) Option {
	return OptionFunc(func(o *Options) {
		o.LogLevel = level
	})
}

// WithLogGroup 日志分组
func WithLogGroup(group string) Option {
	return OptionFunc(func(o *Options) {
		o.LogGroup = group
	})
}

// WithLogAttrs 日志属性
func WithLogAttrs(attrs []Attr) Option {
	return OptionFunc(func(o *Options) {
		o.LogAttrs = attrs
	})
}

// WithFormat 日志格式
func WithFormat(format Format) Option {
	return OptionFunc(func(o *Options) {
		o.Format = format
	})
}

// WithWriter 日志输出
func WithWriter(writer io.Writer) Option {
	if writer == nil {
		writer = os.Stdout
	}
	return OptionFunc(func(o *Options) {
		o.Writer = writer
	})
}

// WithOptions 设置所有配置
func WithOptions(fn func(options *Options)) Option {
	return OptionFunc(func(options *Options) {
		fn(options)
	})
}
