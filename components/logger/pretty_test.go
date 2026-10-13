package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type prettyLogValue func() slog.Value

func (f prettyLogValue) LogValue() slog.Value { return f() }

func TestPrettyExpandsCompositeValues(t *testing.T) {
	type user struct {
		Name  string
		Roles []string
	}
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatPretty))
	l.Info("loaded",
		"user", user{Name: "Alice", Roles: []string{"admin", "editor"}},
		"counts", map[string]int{"active": 2},
		"nil", nil,
		"multiline", "first\nsecond",
	)
	got := out.String()
	for _, want := range []string{
		"level=INFO msg=loaded\nuser => logger.user{",
		`Name:`, `"Alice"`, `Roles:`, "[]string{", `"admin",`,
		"\ncounts => map[string]int{", `"active": 2,`,
		"\nnil => nil", `multiline => "first\nsecond"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("default pretty output contains ANSI colors: %q", got)
	}
}

func TestPrettyGroupsAndContextAttrs(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatPretty)).
		With("service", "api").
		WithGroup("request").With("handler", "login").
		WithGroup("input")
	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "t1"))
	l.InfoContext(ctx, "received",
		slog.Group("http", slog.Int("status", 200)),
		slog.Group("", slog.String("inline", "value")),
		slog.Group("empty"), slog.Attr{},
	)
	got := out.String()
	for _, want := range []string{
		`service => "api"`, `request.handler => "login"`,
		"request.input.http.status => 200",
		`request.input.inline => "value"`, `request.input.trace_id => "t1"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "empty") || strings.Contains(got, "..") || strings.Contains(got, "request.service") {
		t.Fatalf("empty or incorrectly scoped group in output:\n%s", got)
	}
}

func TestPrettyResolvesAndReplacesAttrs(t *testing.T) {
	var out bytes.Buffer
	resolved := false
	var seenGroup string
	l := New(WithWriter(&out), WithFormat(FormatPretty),
		WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case "password":
				resolved = attr.Value.Kind() == slog.KindString && attr.Value.String() == "******"
				return slog.String(attr.Key, "redacted")
			case "token":
				seenGroup = strings.Join(groups, ".")
				return slog.Any(attr.Key, prettyLogValue(func() slog.Value { return slog.StringValue("safe") }))
			case "omit":
				return slog.Attr{}
			default:
				return attr
			}
		}),
	).With("password", testPassword("raw-password")).WithGroup("request")
	ctx := ContextWithAttrs(context.Background(), slog.String("token", "raw-token"))
	l.InfoContext(ctx, "login",
		slog.Group("all_removed", slog.String("omit", "hidden")),
		slog.Any("identity", prettyLogValue(func() slog.Value {
			return slog.GroupValue(slog.String("name", "Alice"))
		})),
	)
	got := out.String()
	if !resolved || seenGroup != "request" {
		t.Fatalf("ReplaceAttr saw unresolved value or incorrect groups: resolved=%v groups=%q", resolved, seenGroup)
	}
	for _, want := range []string{`password => "redacted"`, `request.token => "safe"`, `request.identity.name => "Alice"`} {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{"raw-password", "raw-token", "hidden", "all_removed"} {
		if strings.Contains(got, secret) {
			t.Errorf("output contains removed value %q:\n%s", secret, got)
		}
	}
}

func TestPrettyBuiltinsAndSource(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatPretty), WithAddSource(true),
		WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey || attr.Key == slog.LevelKey || attr.Key == slog.SourceKey || attr.Key == slog.MessageKey {
				if len(groups) != 0 {
					t.Errorf("builtin %q is inside groups %v", attr.Key, groups)
				}
			}
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			if attr.Key == slog.LevelKey {
				return slog.String("severity", attr.Value.Any().(slog.Level).String())
			}
			return attr
		}),
	).WithGroup("request")
	l.Info("source", "id", "r1")
	got := out.String()
	if strings.Contains(got, "time=") || !strings.Contains(got, "severity=INFO source=") || !strings.Contains(got, "pretty_test.go:") {
		t.Fatalf("incorrect builtins or source:\n%s", got)
	}
	if !strings.Contains(got, "msg=source\nrequest.id =>") {
		t.Fatalf("builtin message is incorrectly scoped:\n%s", got)
	}

	out.Reset()
	h := NewHandler(WithWriter(&out), WithFormat(FormatPretty), WithAddSource(true))
	if err := h.Handle(context.Background(), slog.NewRecord(time.Time{}, LevelInfo, "zero", 0)); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "level=INFO msg=zero\n" {
		t.Fatalf("zero time and PC should be omitted: %q", got)
	}
}

func TestPrettyLevelVarSkipsValueResolution(t *testing.T) {
	var out bytes.Buffer
	var level LevelVar
	level.Set(LevelWarn)
	l := New(WithWriter(&out), WithFormat(FormatPretty), WithLevel(&level))
	calls := 0
	value := prettyLogValue(func() slog.Value {
		calls++
		return slog.StringValue("evaluated")
	})
	l.Debug("hidden", "value", value)
	if out.Len() != 0 || calls != 0 {
		t.Fatalf("disabled record was evaluated: calls=%d output=%q", calls, out.String())
	}
	level.Set(LevelDebug)
	l.Debug("shown", "value", value)
	if calls != 1 || !strings.Contains(out.String(), "level=DEBUG msg=shown") {
		t.Fatalf("dynamic level did not enable the record: calls=%d output=%q", calls, out.String())
	}
}

func TestPrettyColorIsOptional(t *testing.T) {
	for _, format := range []Format{FormatPretty, FormatText, FormatJSON} {
		var out bytes.Buffer
		l := New(WithWriter(&out), WithFormat(format), WithColor(true))
		l.Info("colored", "name", "Alice")
		hasColor := strings.Contains(out.String(), "\x1b[")
		if hasColor != (format == FormatPretty) {
			t.Errorf("format=%d color=%v output=%q", format, hasColor, out.String())
		}
	}
}

type prettyCountingWriter struct {
	bytes.Buffer
	writes int
}

func (w *prettyCountingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestPrettyConcurrentDerivedLoggers(t *testing.T) {
	var out prettyCountingWriter
	l := New(WithWriter(&out), WithFormat(FormatPretty))
	const workers, records = 12, 10
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			child := l.WithGroup("worker").With("id", i)
			for j := range records {
				child.Info("record", "values", []int{i, j})
			}
		})
	}
	wg.Wait()
	if out.writes != workers*records || strings.Count(out.String(), "msg=record\n") != workers*records {
		t.Fatalf("records were not written atomically: writes=%d output=%q", out.writes, out.String())
	}
	for _, record := range strings.Split(out.String(), "time=")[1:] {
		if strings.Count(record, "worker.id =>") != 1 || strings.Count(record, "worker.values => []int{") != 1 || !strings.HasSuffix(record, "}\n") {
			t.Fatalf("derived attributes leaked or record was interleaved: %q", record)
		}
	}
	out.Reset()
	l.Info("parent")
	if strings.Contains(out.String(), "worker.") {
		t.Fatalf("child attributes leaked into parent: %q", out.String())
	}
}

type prettyErrorWriter struct{ err error }

func (w prettyErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestPrettyPropagatesWriteErrors(t *testing.T) {
	failure := errors.New("write failed")
	for _, want := range []error{failure, io.ErrShortWrite} {
		writer := prettyErrorWriter{err: failure}
		if want == io.ErrShortWrite {
			writer.err = nil
		}
		h := NewHandler(WithWriter(writer), WithFormat(FormatPretty))
		if err := h.Handle(context.Background(), slog.NewRecord(time.Time{}, LevelInfo, "failure", 0)); !errors.Is(err, want) {
			t.Errorf("Handle error=%v, want %v", err, want)
		}
	}
}
