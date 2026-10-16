package slog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdslog "log/slog"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func testLogger(writer io.Writer) *SlogLogger {
	return NewSlogLogger(stdslog.New(stdslog.NewJSONHandler(writer, &stdslog.HandlerOptions{
		Level: LevelDebug, AddSource: true,
	}))).(*SlogLogger)
}

func logEntries(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	decoder := json.NewDecoder(output)
	for {
		var entry map[string]any
		if err := decoder.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestSetLoggerReplacesGlobalHelpers(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	var oldOutput, newOutput bytes.Buffer
	SetLogger(testLogger(&oldOutput))
	child := With("scope", "child")
	Info("old", stdslog.Int("count", 1))
	Infof("old %s", "formatted")
	replacement := testLogger(&newOutput)
	SetLogger(replacement)
	if L() != replacement || GetLogger() != replacement || Default() != replacement {
		t.Fatal("global accessors did not return the replacement logger")
	}
	Info("new", stdslog.Int("count", 2))
	Infof("new %s", "formatted")
	child.Info("retained child")
	oldEntries, newEntries := logEntries(t, &oldOutput), logEntries(t, &newOutput)
	if len(oldEntries) != 3 || len(newEntries) != 2 {
		t.Fatalf("unexpected old/new entry counts: %d/%d", len(oldEntries), len(newEntries))
	}
	if oldEntries[2]["msg"] != "retained child" || oldEntries[2]["scope"] != "child" {
		t.Fatalf("existing child changed after replacement: %+v", oldEntries[2])
	}
	if newEntries[0]["msg"] != "new" || newEntries[0]["count"] != float64(2) || newEntries[1]["msg"] != "new formatted" {
		t.Fatalf("replacement helpers produced unexpected entries: %+v", newEntries)
	}
}

func TestSetLoggerNil(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	SetLogger(nil)
	if logger, ok := L().(*SlogLogger); !ok || logger == nil || logger.Logger == nil {
		t.Fatal("nil did not install a default logger")
	}
}

func TestSetLoggerNilConcurrent(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 25 {
				SetLogger(nil)
			}
		})
	}
	wg.Wait()
}

type alternateGlobalLogger struct{ Logger }

func TestLogAttrsAcceptsAlternateLogger(t *testing.T) {
	var output bytes.Buffer
	logger := &alternateGlobalLogger{testLogger(&output)}
	logAttrs(logger, context.Background(), LevelInfo, "alternate", stdslog.Int("count", 2))
	entries := logEntries(t, &output)
	if len(entries) != 1 || entries[0]["msg"] != "alternate" || entries[0]["count"] != float64(2) {
		t.Fatalf("logger interface was not used correctly: %+v", entries)
	}
}

func TestSetLoggerRejectsDifferentConcreteType(t *testing.T) {
	previous := L()
	defer func() {
		if recover() == nil {
			t.Error("expected atomic.Value to reject a different concrete type")
		}
		if L() != previous {
			t.Error("failed replacement changed the global logger")
		}
	}()
	SetLogger(&alternateGlobalLogger{previous})
}

func TestGlobalCaller(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	var output bytes.Buffer
	SetLogger(testLogger(&output))
	ctx := context.Background()
	_, file, _, _ := runtime.Caller(0)
	tests := []struct {
		name  string
		log   func()
		panic bool
	}{
		{"direct", func() { L().Info("message") }, false},
		{"directf", func() { L().Infof("%s", "message") }, false},
		{"direct-context", func() { L().InfoContext(ctx, "message") }, false},
		{"direct-debug", func() { L().Debug("message") }, false},
		{"direct-debugf", func() { L().Debugf("%s", "message") }, false},
		{"direct-debug-context", func() { L().DebugContext(ctx, "message") }, false},
		{"direct-warn", func() { L().Warn("message") }, false},
		{"direct-warnf", func() { L().Warnf("%s", "message") }, false},
		{"direct-warn-context", func() { L().WarnContext(ctx, "message") }, false},
		{"direct-error", func() { L().Error("message") }, false},
		{"direct-errorf", func() { L().Errorf("%s", "message") }, false},
		{"direct-error-context", func() { L().ErrorContext(ctx, "message") }, false},
		{"direct-panic", func() { L().Panic("message") }, true},
		{"direct-panicf", func() { L().Panicf("%s", "message") }, true},
		{"direct-panic-context", func() { L().PanicContext(ctx, "message") }, true},
		{"info", func() { Info("message") }, false},
		{"infof", func() { Infof("%s", "message") }, false},
		{"info-context", func() { InfoContext(ctx, "message") }, false},
		{"debug", func() { Debug("message") }, false},
		{"debugf", func() { Debugf("%s", "message") }, false},
		{"debug-context", func() { DebugContext(ctx, "message") }, false},
		{"warn", func() { Warn("message") }, false},
		{"warnf", func() { Warnf("%s", "message") }, false},
		{"warn-context", func() { WarnContext(ctx, "message") }, false},
		{"error", func() { Error("message") }, false},
		{"errorf", func() { Errorf("%s", "message") }, false},
		{"error-context", func() { ErrorContext(ctx, "message") }, false},
		{"panic", func() { Panic("message") }, true},
		{"panicf", func() { Panicf("%s", "message") }, true},
		{"panic-context", func() { PanicContext(ctx, "message") }, true},
		{"with", func() { With("key", "value").Info("message") }, false},
		{"group", func() { WithGroup("group").Info("message", stdslog.Int("count", 1)) }, false},
		{"context-logger", func() { C(WithContext(ctx, L().(*SlogLogger))).Info("message") }, false},
		{"log-attrs", func() { L().(*SlogLogger).LogAttrs(ctx, LevelInfo, "message") }, false},
		{"log-attrs-nil-context", func() { L().(*SlogLogger).LogAttrs(nil, LevelInfo, "message") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output.Reset()
			pc := reflect.ValueOf(tt.log).Pointer()
			_, line := runtime.FuncForPC(pc).FileLine(pc)
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				tt.log()
			}()
			if (recovered != nil) != tt.panic {
				t.Fatalf("unexpected panic: %v", recovered)
			}
			entries := logEntries(t, &output)
			if len(entries) != 1 || entries[0]["msg"] != "message" {
				t.Fatalf("unexpected log entries: %+v", entries)
			}
			source := entries[0]["source"].(map[string]any)
			if source["file"] != file || source["line"] != float64(line) || source["function"] != runtime.FuncForPC(pc).Name() {
				t.Fatalf("expected caller at %s:%d, got %+v", file, line, source)
			}
		})
	}
}

func TestSetLoggerConcurrent(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	var outputs [2]bytes.Buffer
	loggers := []*SlogLogger{
		testLogger(&outputs[0]).With("logger", "a"),
		testLogger(&outputs[1]).With("logger", "b"),
	}
	SetLogger(loggers[0])
	const readers, iterations = 4, 100
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, logger := range loggers {
		wg.Go(func() {
			<-start
			for range iterations {
				SetLogger(logger)
			}
		})
	}
	for range readers {
		wg.Go(func() {
			<-start
			for range iterations {
				logger := L()
				if logger != loggers[0] && logger != loggers[1] {
					t.Error("global accessor returned an unexpected logger")
					return
				}
				logger.Info("direct")
				GetLogger().Info("get")
				Default().Info("default")
				Info("global")
				Infof("formatted %d", 1)
				InfoContext(context.Background(), "context")
			}
		})
	}
	close(start)
	wg.Wait()
	count := 0
	for i := range outputs {
		entries := logEntries(t, &outputs[i])
		count += len(entries)
		for _, entry := range entries {
			if entry["logger"] != string(rune('a'+i)) {
				t.Fatalf("entry lost its logger identity: %+v", entry)
			}
		}
	}
	if count != readers*iterations*6 {
		t.Fatalf("expected %d entries, got %d", readers*iterations*6, count)
	}
}

func TestFatalCaller(t *testing.T) {
	calls := map[string]func(){
		"global":         func() { Fatal("message") },
		"globalf":        func() { Fatalf("%s", "message") },
		"global-context": func() { FatalContext(context.Background(), "message") },
		"direct":         func() { L().Fatal("message") },
		"directf":        func() { L().Fatalf("%s", "message") },
		"direct-context": func() { L().FatalContext(context.Background(), "message") },
	}
	if mode := os.Getenv("GALA_SLOG_FATAL_CALLER"); mode != "" {
		SetLogger(testLogger(os.Stdout))
		calls[mode]()
		t.Fatal("fatal call returned without exiting")
	}
	_, file, _, _ := runtime.Caller(0)
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			pc := reflect.ValueOf(call).Pointer()
			_, line := runtime.FuncForPC(pc).FileLine(pc)
			command := exec.Command(os.Args[0], "-test.run=^TestFatalCaller$")
			command.Env = append(os.Environ(), "GALA_SLOG_FATAL_CALLER="+name)
			output, err := command.CombinedOutput()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				t.Fatalf("expected exit 1, got %v: %s", err, output)
			}
			entries := logEntries(t, bytes.NewBuffer(output))
			if len(entries) != 1 || entries[0]["msg"] != "message" || entries[0]["level"] != "ERROR+4" {
				t.Fatalf("unexpected fatal entry: %+v", entries)
			}
			source := entries[0]["source"].(map[string]any)
			if source["file"] != file || source["line"] != float64(line) {
				t.Fatalf("expected caller at %s:%d, got %+v", file, line, source)
			}
		})
	}
}

func TestContextAndDisabledLogs(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	var output bytes.Buffer
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-1")
	handler := NewHandler(stdslog.NewJSONHandler(&output, &stdslog.HandlerOptions{Level: LevelWarn}),
		WithHandleFunc(func(ctx context.Context, record *stdslog.Record) {
			record.AddAttrs(stdslog.Any("request", ctx.Value(contextKey{})))
		}))
	SetLogger(NewSlogLogger(stdslog.New(handler)))
	Info("disabled")
	L().Info("disabled direct")
	L().(*SlogLogger).LogAttrs(ctx, LevelInfo, "disabled attrs")
	WarnContext(ctx, "enabled", stdslog.Int("count", 2))
	L().WarnContext(ctx, "enabled direct", stdslog.Bool("ready", true))
	entries := logEntries(t, &output)
	if len(entries) != 2 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	for _, entry := range entries {
		if entry["request"] != "request-1" {
			t.Fatalf("context was lost: %+v", entry)
		}
	}
	if entries[0]["count"] != float64(2) || entries[1]["ready"] != true {
		t.Fatalf("attribute types were lost: %+v", entries)
	}
}

func TestNewLoggerCaller(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	var output bytes.Buffer
	SetLogger(NewLogger(WithFormat(FormatJSON), WithWriter(&output)))
	_, file, line, _ := runtime.Caller(0)
	Info("message")
	entries := logEntries(t, &output)
	if len(entries) != 1 || entries[0]["source"] != fmt.Sprintf("%s:%d", getBriefSource(file), line+1) {
		t.Fatalf("unexpected source: %+v", entries)
	}
}
