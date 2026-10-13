package logger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type reviewMasked struct{ secret string }

func (reviewMasked) String() string { return "MASKED" }

type reviewError struct{ secret string }

func (reviewError) Error() string { return "SAFE_ERROR" }

type reviewText struct{ Secret string }

func (reviewText) MarshalText() ([]byte, error) { return []byte("SAFE_TEXT"), nil }

type reviewFormat struct{ Secret string }

func (reviewFormat) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "SAFE_FORMAT") }

func TestReviewValueRepresentations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"stringer", reviewMasked{"RAW_SECRET"}, "MASKED"},
		{"error", reviewError{"RAW_SECRET"}, "SAFE_ERROR"},
		{"text", reviewText{"RAW_SECRET"}, "SAFE_TEXT"},
		{"formatter", reviewFormat{"RAW_SECRET"}, "SAFE_FORMAT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, format := range []Format{FormatText, FormatPretty} {
				var out bytes.Buffer
				New(WithWriter(&out), WithFormat(format)).Info("inspect", "value", tc.value)
				if strings.Contains(out.String(), "RAW_SECRET") || !strings.Contains(out.String(), tc.want) {
					t.Errorf("format=%d ignored author representation: %s", format, out.String())
				}
			}
		})
	}
}

func TestReviewPrivateFields(t *testing.T) {
	value := struct {
		Name   string
		secret string
	}{"visible", "RAW_SECRET"}
	var out bytes.Buffer
	New(WithWriter(&out), WithFormat(FormatPretty)).Info("inspect", "payload", value)
	if strings.Contains(out.String(), "RAW_SECRET") || !strings.Contains(out.String(), "visible") {
		t.Fatalf("private fields were exposed or public data disappeared: %s", out.String())
	}
}

type reviewBrokenText struct{ panic bool }

func (v *reviewBrokenText) MarshalText() ([]byte, error) {
	if v == nil || v.panic {
		panic("broken marshaler")
	}
	return nil, errors.New("marshal failed")
}

func TestReviewTextMarshalFailures(t *testing.T) {
	for _, value := range []*reviewBrokenText{nil, {panic: true}, {}} {
		var out bytes.Buffer
		New(WithWriter(&out), WithFormat(FormatPretty)).Info("inspect", "value", value)
		if !strings.Contains(out.String(), "!ERROR:") {
			t.Fatalf("marshaling failure was not visible: %s", out.String())
		}
	}
}

func TestReviewDurationReadable(t *testing.T) {
	var out bytes.Buffer
	New(WithWriter(&out), WithFormat(FormatPretty)).Info("inspect", "elapsed", 1500*time.Millisecond)
	if !strings.Contains(out.String(), `elapsed => "1.5s"`) {
		t.Fatalf("duration is not readable: %s", out.String())
	}
}

func TestReviewContextNotDuplicated(t *testing.T) {
	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "t1"))
	for _, format := range []Format{FormatPretty, FormatJSON} {
		var out bytes.Buffer
		h := NewHandler(WithWriter(&out), WithFormat(format))
		NewLogger(WrapHandler(h)).InfoContext(ctx, "inspect")
		if strings.Count(out.String(), "trace_id") != 1 {
			t.Errorf("format=%d repeated context extraction: %s", format, out.String())
		}
	}
}

func TestReviewControlKeys(t *testing.T) {
	for _, key := range []string{"\x1b[2J", "\a", "\x00", "\v", "\u2028", "\xff"} {
		var out bytes.Buffer
		New(WithWriter(&out), WithFormat(FormatPretty)).WithGroup(key).Info("inspect", key, "safe")
		if bytes.Contains(out.Bytes(), []byte(key)) {
			t.Errorf("unescaped key %q reached output: %q", key, out.String())
		}
	}
}

func TestReviewGroupSiblingIsolation(t *testing.T) {
	var out bytes.Buffer
	root := New(WithWriter(&out), WithFormat(FormatPretty))
	base := root.WithGroup("a").WithGroup("b").WithGroup("c")
	left := base.WithGroup("left")
	right := base.WithGroup("right")
	left.Info("left", "v", 1)
	right.Info("right", "v", 2)
	if !strings.Contains(out.String(), "a.b.c.left.v => 1") || !strings.Contains(out.String(), "a.b.c.right.v => 2") {
		t.Fatalf("derived group backing arrays were shared: %s", out.String())
	}
}

type reviewProbeWriter struct {
	active     atomic.Int64
	overlapped atomic.Bool
	mu         sync.Mutex
	bytes.Buffer
}

func (w *reviewProbeWriter) Write(p []byte) (int, error) {
	if w.active.Add(1) > 1 {
		w.overlapped.Store(true)
	}
	defer w.active.Add(-1)
	runtime.Gosched()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.Write(p)
}

func TestReviewRecordIsolation(t *testing.T) {
	var out reviewProbeWriter
	root := New(WithWriter(&out), WithFormat(FormatPretty))
	const workers, records = 16, 20
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			child := root.With("worker", i)
			for j := range records {
				child.Info("record", "values", []int{i, j})
			}
		})
	}
	wg.Wait()
	if out.overlapped.Load() {
		t.Error("derived loggers entered the output writer concurrently")
	}
	parts := strings.Split(out.String(), "time=")
	if len(parts)-1 != workers*records {
		t.Fatalf("lost record headers: got=%d", len(parts)-1)
	}
	for _, part := range parts[1:] {
		if strings.Count(part, "worker =>") != 1 || strings.Count(part, "values => []int{") != 1 || !strings.HasSuffix(part, "}\n") {
			t.Fatalf("record contents interleaved: %q", part)
		}
	}
}

func TestReviewReentrantValue(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatPretty))
	done := make(chan struct{})
	go func() {
		l.Info("outer", "value", prettyLogValue(func() slog.Value {
			l.Info("inner")
			return slog.StringValue("resolved")
		}))
		close(done)
	}()
	select {
	case <-done:
		if !strings.Contains(out.String(), "msg=inner") || !strings.Contains(out.String(), "msg=outer") {
			t.Fatalf("reentrant logging dropped a record: %s", out.String())
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("value resolution logged recursively and deadlocked on the output lock")
	}
}

func TestReviewPrinterIsolation(t *testing.T) {
	var colored, plain bytes.Buffer
	first := New(WithWriter(&colored), WithFormat(FormatPretty), WithColor(true))
	second := New(WithWriter(&plain), WithFormat(FormatPretty))
	first.Info("colored", "k", "value")
	second.Info("plain", "k", "value")
	if !strings.Contains(colored.String(), "\x1b[") || strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("printer configuration leaked between loggers: colored=%q plain=%q", colored.String(), plain.String())
	}
}

func TestReviewResolveAfterReplacement(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatPretty), WithReplaceAttr(func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == "payload" {
			return slog.Any(a.Key, prettyLogValue(func() slog.Value { return slog.StringValue("SAFE_REPLACEMENT") }))
		}
		return a
	}))
	l.Info("inspect", "payload", "raw")
	if !strings.Contains(out.String(), `payload => "SAFE_REPLACEMENT"`) {
		t.Fatalf("replacement was not resolved: %s", out.String())
	}
}

func TestReviewGroupBuiltinNames(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatPretty), WithReplaceAttr(func(g []string, a slog.Attr) slog.Attr {
		if len(g) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	})).WithGroup("user")
	l.Info("inspect", "time", "user-time", "source", "user-source")
	if !strings.Contains(out.String(), `user.time => "user-time"`) || !strings.Contains(out.String(), `user.source => "user-source"`) {
		t.Fatalf("user keys were mistaken for builtins: %s", out.String())
	}
}

func BenchmarkReviewBound(b *testing.B) {
	l := New(WithWriter(io.Discard), WithFormat(FormatPretty)).With("payload", struct {
		Name  string
		Roles []string
	}{"Alice", []string{"admin", "editor"}}).WithGroup("request").With("service", "api")
	for b.Loop() {
		l.Info("record")
	}
}

func BenchmarkReviewRecord(b *testing.B) {
	l := New(WithWriter(io.Discard), WithFormat(FormatPretty))
	for b.Loop() {
		l.Info("record", "name", "Alice", "count", 2)
	}
}

func BenchmarkReviewHeader(b *testing.B) {
	l := New(WithWriter(io.Discard), WithFormat(FormatPretty))
	for b.Loop() {
		l.Info("record")
	}
}
