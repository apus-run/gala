package slog

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
)

var global atomic.Value // *SlogLogger

func init() {
	SetLogger(NewLogger())
}

// SetLogger replaces the global logger. It is safe for concurrent use.
// Passing nil installs a new default logger. The concrete type must remain
// *SlogLogger, as installed during initialization and required by atomic.Value.
func SetLogger(logger Logger) {
	if logger == nil {
		logger = NewLogger()
	}
	global.Store(logger)
}

// GetLogger returns the current global logger. It is safe for concurrent use.
func GetLogger() Logger {
	return L()
}

// L 是 GetLogger 简写
func L() Logger {
	return global.Load().(Logger)
}

// Default 是 GetLogger
func Default() Logger {
	return L()
}

func Info(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelInfo, msg, args...)
}

func Infof(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelInfo, sprintf(format, args...))
}

func InfoContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelInfo, msg, args...)
}

func Error(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelError, msg, args...)
}

func Errorf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelError, sprintf(format, args...))
}

func ErrorContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelError, msg, args...)
}

func Debug(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelDebug, msg, args...)
}

func Debugf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelDebug, sprintf(format, args...))
}

func DebugContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelDebug, msg, args...)
}

func Warn(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelWarn, msg, args...)
}

func Warnf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelWarn, sprintf(format, args...))
}

func WarnContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelWarn, msg, args...)
}

func Fatal(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelFatal, msg, args...)
	os.Exit(1)
}

func Fatalf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelFatal, sprintf(format, args...))
	os.Exit(1)
}

func FatalContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelFatal, msg, args...)
	os.Exit(1)
}

func Panic(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelPanic, msg, args...)
	panic(SprintfWithAttrs(msg, args...))
}

func Panicf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelPanic, sprintf(format, args...))
	panic(fmt.Sprintf(format, args...))
}

func PanicContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelPanic, msg, args...)
	panic(SprintfWithAttrs(msg, args...))
}

func With(args ...any) *SlogLogger {
	return L().With(args...)
}

func WithGroup(name string) *SlogLogger {
	return L().WithGroup(name)
}
