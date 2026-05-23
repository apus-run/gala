package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

type testPassword string

func (testPassword) LogValue() slog.Value {
	return slog.StringValue("******")
}

func TestLogWritesJSONLevelsGroupsAndLogValuer(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs.log")
	lumberJackLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   true,
	}
	fixedTime := time.Date(2026, 5, 23, 19, 30, 0, 0, time.UTC)
	l := New(
		WithWriter(lumberJackLogger),
		WithFormat(FormatJSON),
		WithLevel(LevelDebug),
	)

	l.Debug("This is a debug message", slog.Any("key", "value"))
	l.Info("This is a info message")
	l.Warn("This is a warn message")
	l.Error("This is a error message")
	l.Info("WebServer服务信息",
		slog.Group("http",
			slog.Int("status", 200),
			slog.String("method", "POST"),
			slog.Time("time", fixedTime),
		),
	)
	l.Info("敏感数据", slog.Any("password", testPassword("1234567890")))

	if err := lumberJackLogger.Close(); err != nil {
		t.Fatalf("lumberjack.Close() error = %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", logPath, err)
	}
	got := string(content)

	records := decodeJSONLogRecords(t, got)
	if len(records) != 6 {
		t.Fatalf("records = %d, want 6; output = %q", len(records), got)
	}
	assertJSONLogRecord(t, records[0], "DEBUG", "This is a debug message")
	if records[0]["key"] != "value" {
		t.Fatalf("debug record = %#v, want key attr", records[0])
	}
	assertJSONLogRecord(t, records[1], "INFO", "This is a info message")
	assertJSONLogRecord(t, records[2], "WARN", "This is a warn message")
	assertJSONLogRecord(t, records[3], "ERROR", "This is a error message")

	assertJSONLogRecord(t, records[4], "INFO", "WebServer服务信息")
	httpGroup, ok := records[4]["http"].(map[string]any)
	if !ok {
		t.Fatalf("http group = %#v, want object", records[4]["http"])
	}
	if httpGroup["status"] != float64(200) || httpGroup["method"] != "POST" || httpGroup["time"] != fixedTime.Format(time.RFC3339Nano) {
		t.Fatalf("http group = %#v, want status/method/time attrs", httpGroup)
	}

	assertJSONLogRecord(t, records[5], "INFO", "敏感数据")
	if records[5]["password"] != "******" {
		t.Fatalf("password = %#v, want redacted LogValue", records[5]["password"])
	}
	if strings.Contains(got, "1234567890") {
		t.Fatalf("log output = %q, want raw password omitted", got)
	}
}

func TestNewWritesTextLogs(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithLevel(LevelDebug))

	l.Debug("hello", "name", "hermes")

	got := out.String()
	if !strings.Contains(got, "level=DEBUG") || !strings.Contains(got, "msg=hello") || !strings.Contains(got, "name=hermes") {
		t.Fatalf("log output = %q, want debug text log", got)
	}
}

func TestNewHonorsLevel(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithLevel(LevelWarn))

	l.Info("hidden")
	l.Warn("shown")

	got := out.String()
	if strings.Contains(got, "hidden") {
		t.Fatalf("log output = %q, want info filtered", got)
	}
	if !strings.Contains(got, "shown") {
		t.Fatalf("log output = %q, want warn emitted", got)
	}
}

func TestNewWritesJSONLogs(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatJSON))

	l.Info("hello", "name", "hermes")

	got := out.String()
	if !strings.Contains(got, `"level":"INFO"`) || !strings.Contains(got, `"msg":"hello"`) || !strings.Contains(got, `"name":"hermes"`) {
		t.Fatalf("log output = %q, want json log", got)
	}
}

func TestWithReplaceAttrCanRedactSensitiveAttrs(t *testing.T) {
	var out bytes.Buffer
	l := New(
		WithWriter(&out),
		WithFormat(FormatJSON),
		WithReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == "password" || attr.Key == "token" {
				return slog.String(attr.Key, "***")
			}
			return attr
		}),
	)

	l.Info("login",
		"password", "secret",
		slog.Group("user",
			slog.String("token", "abc"),
			slog.String("name", "moocss"),
		),
	)

	got := out.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "abc") {
		t.Fatalf("log output = %q, want secrets redacted", got)
	}
	if strings.Count(got, `"***"`) != 2 {
		t.Fatalf("log output = %q, want two redacted values", got)
	}
	if !strings.Contains(got, "moocss") {
		t.Fatalf("log output = %q, want non-sensitive values preserved", got)
	}
}

func TestPackageHelpersUseDefaultLogger(t *testing.T) {
	origin := Default()
	t.Cleanup(func() { SetDefault(origin) })

	var out bytes.Buffer
	SetDefault(New(WithWriter(&out), WithLevel(LevelDebug)))

	InfoContext(context.Background(), "hello", "name", "default")

	got := out.String()
	if !strings.Contains(got, "msg=hello") || !strings.Contains(got, "name=default") {
		t.Fatalf("log output = %q, want package helper output", got)
	}
}

func TestPackageHelpersExposeDefaultLoggerBehavior(t *testing.T) {
	origin := Default()
	t.Cleanup(func() { SetDefault(origin) })

	var out bytes.Buffer
	SetDefault(New(WithWriter(&out), WithLevel(LevelWarn)))

	if Handler() == nil {
		t.Fatal("Handler() = nil, want default handler")
	}
	if Enabled(context.Background(), LevelInfo) {
		t.Fatal("Enabled(_, LevelInfo) = true, want false")
	}
	if !Enabled(context.Background(), LevelWarn) {
		t.Fatal("Enabled(_, LevelWarn) = false, want true")
	}

	With("component", "test").Warn("with attr")
	WithGroup("request").Warn("with group", "id", "r1")

	got := out.String()
	if !strings.Contains(got, "component=test") || !strings.Contains(got, "request.id=r1") {
		t.Fatalf("log output = %q, want With and WithGroup attrs", got)
	}
}

func TestLogAndLogAttrsUseDefaultLogger(t *testing.T) {
	origin := Default()
	t.Cleanup(func() { SetDefault(origin) })

	capture := &captureHandler{}
	SetDefault(NewLogger(capture))

	Log(context.Background(), LevelInfo, "plain", "k", "v")
	LogAttrs(context.Background(), LevelWarn, "attrs", slog.Int("n", 42))

	records, attrs := capture.snapshot()
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if records[0].Message != "plain" || records[0].Level != LevelInfo || attrs[0]["k"] != "v" {
		t.Fatalf("record[0] = %#v attrs=%#v", records[0], attrs[0])
	}
	if records[1].Message != "attrs" || records[1].Level != LevelWarn || attrs[1]["n"] != int64(42) {
		t.Fatalf("record[1] = %#v attrs=%#v", records[1], attrs[1])
	}
	if records[0].PC == 0 || records[1].PC == 0 {
		t.Fatalf("pc = %d/%d, want caller PCs", records[0].PC, records[1].PC)
	}
}

func TestContextWithAttrsAddsAttrsToContextLogs(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithFormat(FormatJSON))
	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "trace-1"))

	l.InfoContext(ctx, "hello", "name", "context")

	got := out.String()
	if !strings.Contains(got, `"trace_id":"trace-1"`) || !strings.Contains(got, `"name":"context"`) {
		t.Fatalf("log output = %q, want context attrs", got)
	}
}

func TestWithExtractorAddsCustomContextAttrs(t *testing.T) {
	var out bytes.Buffer
	l := NewLogger(
		NewHandler(WithWriter(&out), WithFormat(FormatJSON)),
		WithExtractor(func(ctx context.Context) []slog.Attr {
			return []slog.Attr{slog.String("run_id", "run-1")}
		}),
	)

	l.InfoContext(context.Background(), "hello")

	got := out.String()
	if !strings.Contains(got, `"run_id":"run-1"`) {
		t.Fatalf("log output = %q, want extractor attrs", got)
	}
}

func TestNewLoggerWrapsExistingHandlerWithExtractors(t *testing.T) {
	capture := &captureHandler{}
	l := NewLogger(capture)
	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "trace-1"))

	l.InfoContext(ctx, "hello", "name", "existing")

	records, attrs := capture.snapshot()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if attrs[0]["trace_id"] != "trace-1" || attrs[0]["name"] != "existing" {
		t.Fatalf("attrs = %#v, want trace_id and name", attrs[0])
	}
}

func TestNewLoggerNilHandlerDiscards(t *testing.T) {
	l := NewLogger(nil)

	if l.Enabled(context.Background(), LevelError) {
		t.Fatal("nil-handler logger is enabled, want disabled")
	}
	l.Error("dropped")
}

func TestNewHandlerWithNilExtractorStillLogs(t *testing.T) {
	var out bytes.Buffer
	l := NewLogger(NewHandler(WithWriter(&out)), WithExtractor(nil))

	l.Info("hello")

	if got := out.String(); !strings.Contains(got, "msg=hello") {
		t.Fatalf("log output = %q, want log with nil extractor ignored", got)
	}
}

func TestWrapHandlerAddsContextAttrs(t *testing.T) {
	capture := &captureHandler{}
	handler := WrapHandler(capture)
	l := slog.New(handler)
	ctx := ContextWithAttrs(context.Background(), slog.String("trace_id", "trace-1"))

	l.InfoContext(ctx, "hello")

	records, attrs := capture.snapshot()
	if len(records) != 1 || attrs[0]["trace_id"] != "trace-1" {
		t.Fatalf("records = %#v attrs=%#v, want trace_id attr", records, attrs)
	}
}

func TestWithAddSourceAddsSource(t *testing.T) {
	var out bytes.Buffer
	l := New(WithWriter(&out), WithAddSource(true))

	l.Info("source")

	got := out.String()
	if !strings.Contains(got, "source=") || !strings.Contains(got, "logger_test.go") {
		t.Fatalf("log output = %q, want source file", got)
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]Level{
		"debug":   LevelDebug,
		"INFO":    LevelInfo,
		"warn":    LevelWarn,
		"error":   LevelError,
		"fatal":   LevelFatal,
		"unknown": LevelInfo,
	}

	for input, want := range tests {
		if got := ParseLevel(input); got != want {
			t.Fatalf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

type captureHandler struct {
	parent *captureHandler
	prefix []slog.Attr
	groups []string

	records []slog.Record
	attrs   []map[string]any
}

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *captureHandler) Handle(ctx context.Context, record slog.Record) error {
	attrs := make(map[string]any)
	for _, attr := range h.prefix {
		addAttr(attrs, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(attrs, h.groups, attr)
		return true
	})

	root := h.root()
	root.records = append(root.records, record.Clone())
	root.attrs = append(root.attrs, attrs)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{
		parent: h.root(),
		prefix: appendAttrs(h.prefix, attrs),
		groups: append([]string(nil), h.groups...),
	}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{
		parent: h.root(),
		prefix: append([]slog.Attr(nil), h.prefix...),
		groups: append(append([]string(nil), h.groups...), name),
	}
}

func (h *captureHandler) snapshot() ([]slog.Record, []map[string]any) {
	root := h.root()
	records := make([]slog.Record, len(root.records))
	copy(records, root.records)
	attrs := make([]map[string]any, len(root.attrs))
	copy(attrs, root.attrs)
	return records, attrs
}

func (h *captureHandler) root() *captureHandler {
	if h.parent == nil {
		return h
	}
	return h.parent.root()
}

func appendAttrs(base []slog.Attr, extra []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}

func addAttr(attrs map[string]any, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		nextGroups := append([]string{}, groups...)
		nextGroups = append(nextGroups, attr.Key)
		for _, groupAttr := range attr.Value.Group() {
			addAttr(attrs, nextGroups, groupAttr)
		}
		return
	}
	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(append(append([]string{}, groups...), attr.Key), ".")
	}
	attrs[key] = attr.Value.Any()
}

func TestCaptureHandlerWithAttrsAndGroup(t *testing.T) {
	h := (&captureHandler{}).
		WithGroup("request").
		WithAttrs([]slog.Attr{
			slog.String("component", "logger"),
			slog.String("id", "r1"),
		})

	record := slog.NewRecord(time.Now(), LevelInfo, "msg", callerPC())
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	records, attrs := h.(*captureHandler).snapshot()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if attrs[0]["request.component"] != "logger" || attrs[0]["request.id"] != "r1" {
		t.Fatalf("attrs = %#v", attrs[0])
	}
}

func callerPC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	return pcs[0]
}

func decodeJSONLogRecords(t *testing.T, output string) []map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(output), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("json.Unmarshal(%q) error = %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func assertJSONLogRecord(t *testing.T, record map[string]any, level, msg string) {
	t.Helper()

	if record["level"] != level || record["msg"] != msg {
		t.Fatalf("record = %#v, want level=%q msg=%q", record, level, msg)
	}
}
