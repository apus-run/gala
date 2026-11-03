package slog

import (
	"context"
	"log/slog"
)

// Logger is the logging contract shared by slog-based loggers | 基于 slog 的日志器共同实现的接口.
type Logger interface {
	Info(msg string, attrs ...Attr)
	Infof(format string, attrs ...any)
	InfoContext(ctx context.Context, msg string, attrs ...Attr)
	Error(msg string, attrs ...Attr)
	Errorf(format string, args ...any)
	ErrorContext(ctx context.Context, msg string, attrs ...Attr)
	Debug(msg string, attrs ...Attr)
	Debugf(format string, args ...any)
	DebugContext(ctx context.Context, msg string, attrs ...Attr)
	Warn(msg string, attrs ...Attr)
	Warnf(format string, args ...any)
	WarnContext(ctx context.Context, msg string, attrs ...Attr)
	Panic(msg string, attrs ...Attr)
	Panicf(format string, args ...any)
	PanicContext(ctx context.Context, msg string, attrs ...Attr)
	Fatal(msg string, attrs ...Attr)
	Fatalf(format string, args ...any)
	FatalContext(ctx context.Context, msg string, attrs ...Attr)
	With(args ...any) *SlogLogger
	WithGroup(name string) *SlogLogger

	Handler() slog.Handler
}

// Attr is an alias of slog.Attr | Attr 是 slog.Attr 的别名.
type Attr = slog.Attr

// Format selects the log output encoding | Format 选择日志输出编码.
type Format string

const (
	// FormatText selects logfmt-style text output | 表示 logfmt 风格的文本输出.
	FormatText Format = "text"
	// FormatJSON selects JSON output | 表示 JSON 输出.
	FormatJSON Format = "json"
)
