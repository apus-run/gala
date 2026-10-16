package zlog

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestGlobalLogInit(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	logger := NewLogger(WithFormat(FormatJSON))
	SetLogger(logger)

	Info("test info")
	Error("test error")
	Warn("test warn")

	Error("处理业务逻辑出错",
		zap.String("path", "/global.go"),
		// 命中的路由
		zap.String("route", "/hello"),
		zap.Error(errors.New("自定义错误")),
		zap.Time("time", time.Now()),
		zap.Duration("duration", time.Duration(int64(10))),
	)

	Info("test info")
}

func TestGlobalLog(t *testing.T) {
	L().Info("test info")
	L().Error("test error")
	L().Warn("test warn")

	L().Error("处理业务逻辑出错",
		zap.String("path", "/global.go"),
		// 命中的路由
		zap.String("route", "/hello"),
		zap.Error(errors.New("自定义错误")),
		zap.Time("time", time.Now()),
		zap.Duration("duration", time.Duration(int64(10))),
	)
}

func TestZapLoggerReturnsUnderlyingLogger(t *testing.T) {
	underlying := zap.NewNop()
	logger := NewZapLogger(underlying)
	if logger.Zap() != underlying || logger.Zap() != logger.Zap() {
		t.Fatal("Zap did not return the original logger")
	}
}

func TestContext(t *testing.T) {
	c := WithContext(context.Background(), "name", "foo")

	C(c).Info("test info")
	C(c).Error("test error")
	C(c).Warn("test warn")
	C(c).Error("处理业务逻辑出错",
		zap.String("path", "/global.go"),
		// 命中的路由
		zap.String("route", "/hello"),
		zap.Error(errors.New("自定义错误")),
		zap.Time("time", time.Now()),
		zap.Duration("duration", time.Duration(int64(10))),
	)
}

type contextLogger struct{ Logger }

func TestWithContextUsesLoggerInterface(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := (&ZapLogger{buildZapLogger(core, "prod")}).With(zap.String("scope", "original"))
	ctx := context.WithValue(context.Background(), contextLogKey{}, &contextLogger{logger})
	ctx = WithContext(ctx, "count", 2)
	FromContext(ctx).Info("message")
	entries := logs.All()
	if len(entries) != 1 || entries[0].Message != "message" || entries[0].ContextMap()["scope"] != "original" || entries[0].ContextMap()["count"] != int64(2) {
		t.Fatalf("context logger or fields were lost: %+v", entries)
	}
}

func TestSetLoggerNil(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	SetLogger(nil)
	if logger, ok := L().(*ZapLogger); !ok || logger == nil || logger.Zap() == nil {
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
	SetLogger(&contextLogger{previous})
}

func TestSetLoggerReplacesGlobalHelpers(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })

	oldCore, oldLogs := observer.New(zap.DebugLevel)
	oldLogger := &ZapLogger{buildZapLogger(oldCore, "prod")}
	SetLogger(oldLogger)
	child := With(zap.String("scope", "child"))
	Info("old")
	Infof("old %s", "formatted")

	newCore, newLogs := observer.New(zap.DebugLevel)
	newLogger := &ZapLogger{buildZapLogger(newCore, "prod")}
	SetLogger(newLogger)
	if L() != newLogger || GetLogger() != newLogger || Default() != newLogger {
		t.Fatal("global accessors did not return the replacement logger")
	}
	Info("new", zap.String("key", "value"), zap.Int("count", 2))
	Infof("new %s", "formatted")
	child.Info("retained child")

	oldEntries, newEntries := oldLogs.All(), newLogs.All()
	if len(oldEntries) != 3 || len(newEntries) != 2 {
		t.Fatalf("unexpected old/new entry counts: %d/%d", len(oldEntries), len(newEntries))
	}
	if oldEntries[2].Message != "retained child" || oldEntries[2].ContextMap()["scope"] != "child" {
		t.Fatalf("existing child logger changed after replacement: %+v", oldEntries[2])
	}
	if newEntries[0].Message != "new" || newEntries[0].ContextMap()["key"] != "value" || newEntries[1].Message != "new formatted" {
		t.Fatalf("replacement helpers produced unexpected entries: %+v", newEntries)
	}
	if newEntries[0].ContextMap()["count"] != int64(2) {
		t.Fatalf("package logger did not preserve the typed field: %+v", newEntries[0])
	}
}

func TestGlobalCaller(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	core, logs := observer.New(zap.DebugLevel)
	SetLogger(&ZapLogger{buildZapLogger(core, "prod").WithOptions(
		zap.WithFatalHook(zapcore.WriteThenPanic),
	)})
	_, file, _, _ := runtime.Caller(0)
	ctx := WithContext(context.Background(), "scope", "context")
	extractors := map[string]func(context.Context) string{
		"scope": func(context.Context) string { return "context" },
	}

	tests := []struct {
		name  string
		log   func()
		level zapcore.Level
		panic bool
	}{
		{"direct", func() { L().Info("message") }, zap.InfoLevel, false},
		{"directf", func() { L().Infof("%s", "message") }, zap.InfoLevel, false},
		{"print", func() { L().Print("message") }, zap.InfoLevel, false},
		{"printf", func() { L().Printf("%s", "message") }, zap.InfoLevel, false},
		{"stat", func() { L().Stat("message") }, zap.InfoLevel, false},
		{"slow", func() { L().Slow("message") }, zap.WarnLevel, false},
		{"stack", func() { L().Stack("message") }, zap.ErrorLevel, false},
		{"with", func() { With(zap.String("key", "value")).Info("message") }, zap.InfoLevel, false},
		{"from-context", func() { FromContext(ctx).Info("message") }, zap.InfoLevel, false},
		{"context", func() { C(ctx).Info("message") }, zap.InfoLevel, false},
		{"contextf", func() { C(ctx).Infof("%s", "message") }, zap.InfoLevel, false},
		{"with-context", func() { FromContext(WithContext(ctx, "key", "value")).Info("message") }, zap.InfoLevel, false},
		{"w", func() { W(WithRequestID(ctx, "request-1")).Info("message") }, zap.InfoLevel, false},
		{"wc", func() { WC(ctx, extractors).Info("message") }, zap.InfoLevel, false},
		{"debug", func() { Debug("message") }, zap.DebugLevel, false},
		{"info", func() { Info("message") }, zap.InfoLevel, false},
		{"warn", func() { Warn("message") }, zap.WarnLevel, false},
		{"error", func() { Error("message") }, zap.ErrorLevel, false},
		{"panic", func() { Panic("message") }, zap.PanicLevel, true},
		{"fatal", func() { Fatal("message") }, zap.FatalLevel, true},
		{"debugf", func() { Debugf("%s", "message") }, zap.DebugLevel, false},
		{"infof", func() { Infof("%s", "message") }, zap.InfoLevel, false},
		{"warnf", func() { Warnf("%s", "message") }, zap.WarnLevel, false},
		{"errorf", func() { Errorf("%s", "message") }, zap.ErrorLevel, false},
		{"panicf", func() { Panicf("%s", "message") }, zap.PanicLevel, true},
		{"fatalf", func() { Fatalf("%s", "message") }, zap.FatalLevel, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			entries := logs.TakeAll()
			if len(entries) != 1 {
				t.Fatalf("expected one entry, got %d", len(entries))
			}
			entry := entries[0]
			if entry.Message != "message" || entry.Level != tt.level {
				t.Fatalf("unexpected entry: %+v", entry)
			}
			if !entry.Caller.Defined || entry.Caller.File != file || entry.Caller.Line != line || !strings.Contains(entry.Caller.Function, ".TestGlobalCaller.") {
				t.Fatalf("expected caller at %s:%d in the test's logging function, got %+v", file, line, entry.Caller)
			}
		})
	}
}

func TestSetLoggerConcurrent(t *testing.T) {
	previous := L()
	t.Cleanup(func() { SetLogger(previous) })
	core, logs := observer.New(zap.DebugLevel)
	loggers := []*ZapLogger{
		{zap.New(core).With(zap.String("logger", "a"))},
		{zap.New(core).With(zap.String("logger", "b"))},
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
					t.Error("global accessor returned an unexpected logger during replacement")
					return
				}
				logger.Info("direct")
				GetLogger().Info("get")
				Default().Info("default")
				Info("global")
				Infof("formatted %d", 1)
			}
		})
	}
	close(start)
	wg.Wait()
	if got, want := logs.Len(), readers*iterations*5; got != want {
		t.Fatalf("expected %d entries, got %d", want, got)
	}
	for _, entry := range logs.All() {
		if value := entry.ContextMap()["logger"]; value != "a" && value != "b" {
			t.Fatalf("entry lost its logger fields: %+v", entry)
		}
	}
	// Once all writers finish, every entry point must use the same logger.
	logs.TakeAll()
	L().Info("direct")
	Info("global")
	Infof("formatted %d", 1)
	entries := logs.All()
	for _, entry := range entries[1:] {
		if entry.ContextMap()["logger"] != entries[0].ContextMap()["logger"] {
			t.Fatalf("logging entry points disagree after replacement: %+v", entries)
		}
	}
}
