package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Format selects the handler output format | Format 选择 handler 输出格式.
type Format int

const (
	// FormatText is slog's default text format | slog 默认文本格式.
	FormatText Format = iota
	// FormatJSON is slog's JSON format | slog JSON 格式.
	FormatJSON
	// FormatPretty expands attribute values with github.com/k0kubun/pp/v3.
	// FormatPretty 用 k0kubun/pp/v3 展开属性值。
	FormatPretty
)

// HandlerOption configures the base handler | HandlerOption 配置基础 handler。
type HandlerOption interface {
	apply(*handlerConfig)
}

// HandlerOptionFunc adapts a plain function to HandlerOption.
// HandlerOptionFunc 把普通函数适配为 HandlerOption。
type HandlerOptionFunc func(*handlerConfig)

func (f HandlerOptionFunc) apply(o *handlerConfig) {
	f(o)
}

// WrapperOption configures the context wrapper | WrapperOption 配置 context 包装层。
type WrapperOption interface {
	apply(*wrapperConfig)
}

// WrapperOptionFunc adapts a plain function to WrapperOption.
// WrapperOptionFunc 把普通函数适配为 WrapperOption。
type WrapperOptionFunc func(*wrapperConfig)

func (f WrapperOptionFunc) apply(o *wrapperConfig) {
	f(o)
}

// Extractor pulls attrs out of a context | Extractor 从 context 中提取属性。
type Extractor func(context.Context) []slog.Attr

type handlerConfig struct {
	writer      io.Writer
	format      Format
	level       Leveler
	addSource   bool
	color       bool
	replaceAttr func(groups []string, attr slog.Attr) slog.Attr
}

type wrapperConfig struct {
	extractors []Extractor
}

// New builds a logger with the configured base handler
// | New 按配置的基础 handler 构建日志器。
func New(opts ...HandlerOption) *slog.Logger {
	return slog.New(NewHandler(opts...))
}

// NewHandler builds the base handler wrapped with context attr injection
// | NewHandler 构建基础 handler 并包上 context 属性注入。
func NewHandler(opts ...HandlerOption) slog.Handler {
	cfg := &handlerConfig{
		writer: os.Stderr,
		format: FormatText,
		level:  LevelInfo,
	}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	return WrapHandler(newBaseHandler(cfg))
}

// NewLogger wraps a caller-supplied handler | NewLogger 包装调用方提供的 handler。
func NewLogger(handler slog.Handler, opts ...WrapperOption) *slog.Logger {
	return slog.New(WrapHandler(handler, opts...))
}

// WrapHandler injects context-derived attrs into any slog.Handler
// | WrapHandler 给任意 slog.Handler 注入从 context 提取的属性。
func WrapHandler(handler slog.Handler, opts ...WrapperOption) slog.Handler {
	cfg := &wrapperConfig{extractors: []Extractor{AttrsFromContext}}
	if _, wrapped := handler.(*contextHandler); wrapped {
		cfg.extractors = nil // The existing wrapper already extracts stored attrs. | 已有包装层会提取存储的属性
	}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	return newComposedHandler(handler, cfg)
}

// WithWriter sets the output writer | WithWriter 设置输出 writer。
func WithWriter(w io.Writer) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.writer = w })
}

// WithFormat selects the output format | WithFormat 选择输出格式。
func WithFormat(format Format) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.format = format })
}

// WithColor enables ANSI colors for attribute values in FormatPretty.
// Colors are disabled by default. Other formats ignore this option.
// WithColor 在 FormatPretty 下开启属性值的 ANSI 颜色。默认关闭；
// 其他格式忽略该选项。
func WithColor(enabled bool) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.color = enabled })
}

// WithLevel sets the minimum enabled level | WithLevel 设置最小启用级别。
func WithLevel(level Leveler) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.level = level })
}

// WithAddSource toggles the source location field | WithAddSource 开关来源位置字段。
func WithAddSource(add bool) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.addSource = add })
}

// WithReplaceAttr rewrites attrs before output | WithReplaceAttr 在输出前改写属性。
func WithReplaceAttr(fn func(groups []string, attr slog.Attr) slog.Attr) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.replaceAttr = fn })
}

// WithExtractor appends context extractors | WithExtractor 追加 context 提取器。
func WithExtractor(extractors ...Extractor) WrapperOption {
	return WrapperOptionFunc(func(c *wrapperConfig) {
		for _, extractor := range extractors {
			if extractor != nil {
				c.extractors = append(c.extractors, extractor)
			}
		}
	})
}

func newComposedHandler(handler slog.Handler, cfg *wrapperConfig) slog.Handler {
	if handler == nil {
		return discardHandler{}
	}
	return newContextHandler(handler, cfg.extractors...)
}

func newBaseHandler(cfg *handlerConfig) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:       cfg.level,
		AddSource:   cfg.addSource,
		ReplaceAttr: cfg.replaceAttr,
	}
	switch cfg.format {
	case FormatJSON:
		return slog.NewJSONHandler(cfg.writer, opts)
	case FormatPretty:
		return newPrettyHandler(cfg.writer, *opts, cfg.color)
	}
	return slog.NewTextHandler(cfg.writer, opts)
}
