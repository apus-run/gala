package logger

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestContextWithAttrsAppendsAttrs(t *testing.T) {
	ctx := ContextWithAttrs(context.Background(), slog.String("a", "1"))
	ctx = ContextWithAttrs(ctx, slog.String("b", "2"))

	attrs := AttrsFromContext(ctx)
	if len(attrs) != 2 {
		t.Fatalf("len(attrs) = %d, want 2", len(attrs))
	}
	if attrs[0].Key != "a" || attrs[0].Value.String() != "1" {
		t.Fatalf("attrs[0] = %v", attrs[0])
	}
	if attrs[1].Key != "b" || attrs[1].Value.String() != "2" {
		t.Fatalf("attrs[1] = %v", attrs[1])
	}
}

func TestContextWithAttrsNilCtx(t *testing.T) {
	ctx := ContextWithAttrs(nil, slog.Int("n", 1)) //nolint:staticcheck
	if attrs := AttrsFromContext(ctx); len(attrs) != 1 {
		t.Fatalf("len(attrs) = %d, want 1", len(attrs))
	}
}

func TestContextWithAttrsNoAttrsReturnsSameContext(t *testing.T) {
	ctx := context.Background()
	if got := ContextWithAttrs(ctx); got != ctx {
		t.Fatal("ContextWithAttrs(ctx) returned a different context for no attrs")
	}
}

func TestAttrsFromContextNilCtx(t *testing.T) {
	if attrs := AttrsFromContext(nil); attrs != nil { //nolint:staticcheck
		t.Fatalf("AttrsFromContext(nil) = %v, want nil", attrs)
	}
}

func TestContextHandlerMerges(t *testing.T) {
	capture := &captureHandler{}
	h := newContextHandler(capture, AttrsFromContext)

	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "t1"))
	record := slog.NewRecord(now(), LevelInfo, "msg", 0)
	record.AddAttrs(slog.String("k", "v"))
	if err := h.Handle(ctx, record); err != nil {
		t.Fatalf("handle: %v", err)
	}
	records, attrs := capture.snapshot()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if got := attrs[0]["trace_id"]; got != "t1" {
		t.Fatalf("trace_id = %v", got)
	}
	if got := attrs[0]["k"]; got != "v" {
		t.Fatalf("k = %v", got)
	}
}

func TestContextHandlerNoAttrs(t *testing.T) {
	capture := &captureHandler{}
	h := newContextHandler(capture, AttrsFromContext)
	record := slog.NewRecord(now(), LevelInfo, "msg", 0)
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle: %v", err)
	}
	records, attrs := capture.snapshot()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if len(attrs[0]) != 0 {
		t.Fatalf("attrs = %#v, want empty attrs", attrs[0])
	}
}

func TestContextHandlerNoExtractorsReturnsNext(t *testing.T) {
	capture := &captureHandler{}
	if got := newContextHandler(capture, nil); got != capture {
		t.Fatalf("newContextHandler() = %T, want original handler", got)
	}
}

func TestContextHandlerCustomExtractor(t *testing.T) {
	capture := &captureHandler{}
	h := newContextHandler(
		capture,
		AttrsFromContext,
		func(ctx context.Context) []slog.Attr {
			return []slog.Attr{slog.String("run_id", "r1")}
		},
	)

	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "t1"))
	record := slog.NewRecord(now(), LevelInfo, "msg", 0)
	if err := h.Handle(ctx, record); err != nil {
		t.Fatalf("handle: %v", err)
	}

	_, attrs := capture.snapshot()
	if got := attrs[0]["trace_id"]; got != "t1" {
		t.Fatalf("trace_id = %v", got)
	}
	if got := attrs[0]["run_id"]; got != "r1" {
		t.Fatalf("run_id = %v", got)
	}
}

func TestContextHandlerMergesNestedExtractors(t *testing.T) {
	capture := &captureHandler{}
	first := newContextHandler(capture, func(ctx context.Context) []slog.Attr {
		return []slog.Attr{slog.String("a", "1")}
	})
	second := newContextHandler(first, func(ctx context.Context) []slog.Attr {
		return []slog.Attr{slog.String("b", "2")}
	})

	record := slog.NewRecord(now(), LevelInfo, "msg", 0)
	if err := second.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle: %v", err)
	}

	_, attrs := capture.snapshot()
	if got := attrs[0]["a"]; got != "1" {
		t.Fatalf("a = %v", got)
	}
	if got := attrs[0]["b"]; got != "2" {
		t.Fatalf("b = %v", got)
	}
}

func TestContextHandlerWithAttrsAndGroupKeepsExtractors(t *testing.T) {
	capture := &captureHandler{}
	h := newContextHandler(capture, AttrsFromContext).
		WithGroup("request").
		WithAttrs([]slog.Attr{slog.String("handler", "h1")})

	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "t1"))
	record := slog.NewRecord(now(), LevelInfo, "msg", 0)
	record.AddAttrs(slog.String("k", "v"))
	if err := h.Handle(ctx, record); err != nil {
		t.Fatalf("handle: %v", err)
	}

	_, attrs := capture.snapshot()
	if got := attrs[0]["request.handler"]; got != "h1" {
		t.Fatalf("request.handler = %v", got)
	}
	if got := attrs[0]["request.trace_id"]; got != "t1" {
		t.Fatalf("request.trace_id = %v", got)
	}
	if got := attrs[0]["request.k"]; got != "v" {
		t.Fatalf("request.k = %v", got)
	}
}

func now() time.Time { return time.Now() }
