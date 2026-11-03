package logger

import (
	"context"
	"log/slog"
)

type ctxAttrsKey struct{}

// ContextWithAttrs stores attrs in ctx, merging with any already stored
// | ContextWithAttrs 把属性存入 ctx，与已有属性合并。
func ContextWithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(attrs) == 0 {
		return ctx
	}
	prev := AttrsFromContext(ctx)
	merged := make([]slog.Attr, 0, len(prev)+len(attrs))
	merged = append(merged, prev...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, ctxAttrsKey{}, merged)
}

// AttrsFromContext returns the attrs stored in ctx | AttrsFromContext 取出 ctx 中保存的属性。
func AttrsFromContext(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(ctxAttrsKey{}).([]slog.Attr)
	return attrs
}

func newContextHandler(next slog.Handler, extractors ...Extractor) slog.Handler {
	if next == nil {
		next = discardHandler{}
	}
	extractors = compactExtractors(extractors)
	if len(extractors) == 0 {
		return next
	}
	if h, ok := next.(*contextHandler); ok {
		merged := make([]Extractor, 0, len(h.extractors)+len(extractors))
		merged = append(merged, h.extractors...)
		merged = append(merged, extractors...)
		return &contextHandler{next: h.next, extractors: merged}
	}
	return &contextHandler{next: next, extractors: extractors}
}

func compactExtractors(extractors []Extractor) []Extractor {
	out := extractors[:0]
	for _, fn := range extractors {
		if fn != nil {
			out = append(out, fn)
		}
	}
	return out
}

type contextHandler struct {
	next       slog.Handler
	extractors []Extractor
}

// Enabled implements slog.Handler | 实现 slog.Handler.
func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle implements slog.Handler, injecting the extracted attrs | 实现 slog.Handler，注入提取到的属性.
func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := h.attrs(ctx)
	if len(attrs) > 0 {
		record = record.Clone()
		record.AddAttrs(attrs...)
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs implements slog.Handler | 实现 slog.Handler.
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs), extractors: h.extractors}
}

// WithGroup implements slog.Handler | 实现 slog.Handler.
func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name), extractors: h.extractors}
}

func (h *contextHandler) attrs(ctx context.Context) []slog.Attr {
	var attrs []slog.Attr
	for _, fn := range h.extractors {
		attrs = append(attrs, fn(ctx)...)
	}
	return attrs
}
