package zlog

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/natefinch/lumberjack.v2"
)

func getLogWriter(t *testing.T) io.Writer {
	t.Helper()

	lumberJackLogger := &lumberjack.Logger{
		Filename:   filepath.Join(t.TempDir(), "logs.log"),
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   true,
	}
	t.Cleanup(func() {
		_ = lumberJackLogger.Close()
	})
	return lumberJackLogger
}

func TestLog(t *testing.T) {
	wr := getLogWriter(t)
	logger := NewLogger(WithFormat(FormatJSON), WithWriters(os.Stdout, wr))
	defer logger.Close()

	logger.Info("This is an info message", zap.String("route", "/hello"), zap.Int64("port", 8090))
	logger.Infof("我是日志: %v, %v", zap.String("route", "/hello"), zap.Int64("port", 8090))
	logger.Error("This is an error message")
}

func TestZapLoggerPreservesFieldTypes(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := &ZapLogger{zap.New(core)}
	fields := []Field{
		zap.Int("count", 2),
		zap.Bool("ready", true),
		zap.Error(errors.New("boom")),
		zap.String("key", "value"),
	}
	logger.Info("direct", fields...)
	logger.With(fields...).Info("child")

	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("expected two entries, got %d", len(entries))
	}
	for _, entry := range entries {
		values := entry.ContextMap()
		if values["count"] != int64(2) || values["ready"] != true || values["error"] != "boom" || values["key"] != "value" {
			t.Fatalf("logger changed field types or values: %+v", entry)
		}
	}
}

// 性能测试用例
func BenchmarkZapLoggerW(b *testing.B) {
	// 创建一个 zapLogger 实例（使用 zap.NewNop() 模拟 logger）
	logger := &ZapLogger{zap.NewNop()}

	// 创建一个包含上下文值的 context
	ctx := WithRequestID(context.Background(), "request-id-12345")
	ctx = WithUserID(ctx, "user-id-67890")

	// 重复调用 W 函数，测量性能
	b.ResetTimer()
	for b.Loop() {
		_ = logger.W(ctx)
	}
}
