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
)

type HandlerOption func(*handlerConfig)
type WrapperOption func(*wrapperConfig)

type Extractor func(context.Context) []slog.Attr

type handlerConfig struct {
	writer      io.Writer
	format      Format
	level       Leveler
	addSource   bool
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
		opt(cfg)
	}
	return WrapHandler(newBaseHandler(cfg))
}

func NewLogger(handler slog.Handler, opts ...WrapperOption) *slog.Logger {
	return slog.New(WrapHandler(handler, opts...))
}

func WrapHandler(handler slog.Handler, opts ...WrapperOption) slog.Handler {
	cfg := &wrapperConfig{extractors: []Extractor{AttrsFromContext}}
	for _, opt := range opts {
		opt(cfg)
	}
	return newComposedHandler(handler, cfg)
}

func WithWriter(w io.Writer) HandlerOption {
	return func(c *handlerConfig) { c.writer = w }
}

func WithFormat(format Format) HandlerOption {
	return func(c *handlerConfig) { c.format = format }
}

func WithLevel(level Leveler) HandlerOption {
	return func(c *handlerConfig) { c.level = level }
}

func WithAddSource(add bool) HandlerOption {
	return func(c *handlerConfig) { c.addSource = add }
}

func WithReplaceAttr(fn func(groups []string, attr slog.Attr) slog.Attr) HandlerOption {
	return func(c *handlerConfig) { c.replaceAttr = fn }
}

func WithExtractor(extractors ...Extractor) WrapperOption {
	return func(c *wrapperConfig) {
		for _, extractor := range extractors {
			if extractor != nil {
				c.extractors = append(c.extractors, extractor)
			}
		}
	}
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
	if cfg.format == FormatJSON {
		return slog.NewJSONHandler(cfg.writer, opts)
	}
	return slog.NewTextHandler(cfg.writer, opts)
}
