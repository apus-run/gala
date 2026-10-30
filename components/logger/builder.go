package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
)

type Format int

const (
	FormatText Format = iota
	FormatJSON
	// FormatPretty expands attribute values with github.com/k0kubun/pp/v3.
	FormatPretty
)

type HandlerOption interface {
	apply(*handlerConfig)
}

// HandlerOptionFunc adapts a plain function to HandlerOption.
type HandlerOptionFunc func(*handlerConfig)

func (f HandlerOptionFunc) apply(o *handlerConfig) {
	f(o)
}

type WrapperOption interface {
	apply(*wrapperConfig)
}

// WrapperOptionFunc adapts a plain function to WrapperOption.
type WrapperOptionFunc func(*wrapperConfig)

func (f WrapperOptionFunc) apply(o *wrapperConfig) {
	f(o)
}

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

func New(opts ...HandlerOption) *slog.Logger {
	return slog.New(NewHandler(opts...))
}

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

func NewLogger(handler slog.Handler, opts ...WrapperOption) *slog.Logger {
	return slog.New(WrapHandler(handler, opts...))
}

func WrapHandler(handler slog.Handler, opts ...WrapperOption) slog.Handler {
	cfg := &wrapperConfig{extractors: []Extractor{AttrsFromContext}}
	if _, wrapped := handler.(*contextHandler); wrapped {
		cfg.extractors = nil // The existing wrapper already extracts stored attrs.
	}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	return newComposedHandler(handler, cfg)
}

func WithWriter(w io.Writer) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.writer = w })
}

func WithFormat(format Format) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.format = format })
}

// WithColor enables ANSI colors for attribute values in FormatPretty.
// Colors are disabled by default. Other formats ignore this option.
func WithColor(enabled bool) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.color = enabled })
}

func WithLevel(level Leveler) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.level = level })
}

func WithAddSource(add bool) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.addSource = add })
}

func WithReplaceAttr(fn func(groups []string, attr slog.Attr) slog.Attr) HandlerOption {
	return HandlerOptionFunc(func(c *handlerConfig) { c.replaceAttr = fn })
}

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
