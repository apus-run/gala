package zlog

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the logging contract shared by zap-based loggers | 基于 zap 的日志器共同实现的接口.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Panicf(format string, args ...any)

	Info(msg string, tags ...Field)
	Error(msg string, tags ...Field)
	Debug(msg string, tags ...Field)
	Warn(msg string, tags ...Field)
	Fatal(msg string, tags ...Field)
	Panic(msg string, tags ...Field)

	Slow(msg string, fields ...Field)
	Stack(msg string)
	Stat(msg string, fields ...Field)

	Print(args ...any)
	Printf(format string, args ...any)

	With(fields ...Field) Logger
	AddCallerSkip(skip int) Logger

	Close() error
	Sync() error

	W(ctx context.Context) Logger
	WC(ctx context.Context, contextExtractors map[string]func(context.Context) string) Logger

	WithContext(ctx context.Context, keyvals ...any) context.Context

	Zap() *zap.Logger
	Sugar() *zap.SugaredLogger
}

// Field is an alias of zapcore.Field | Field 是 zapcore.Field 的别名.
type Field = zapcore.Field

// Format selects the log output encoding | Format 选择日志输出编码.
type Format string

const (
	// FormatText selects console-style text output | 表示控制台风格的文本输出.
	FormatText Format = "text"
	// FormatJSON selects JSON output | 表示 JSON 输出.
	FormatJSON Format = "json"
)
