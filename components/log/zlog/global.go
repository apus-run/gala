package zlog

import (
	"context"
	"sync/atomic"
)

var global atomic.Value // *ZapLogger

func init() {
	SetLogger(NewLogger())
}

// SetLogger replaces the global logger. It is safe for concurrent use.
// Passing nil installs a new default logger.
// The concrete type must remain *ZapLogger, as installed during initialization
// and required by atomic.Value.
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

// Debugf 输出 debug 级别的日志.
func Debugf(format string, args ...any) {
	L().Sugar().Debugf(format, args...)
}

// Debug 输出 debug 级别的日志.
func Debug(msg string, tags ...Field) {
	L().Zap().Debug(msg, tags...)
}

// Infof 输出 info 级别的日志.
func Infof(format string, args ...any) {
	L().Sugar().Infof(format, args...)
}

// Info 输出 info 级别的日志.
func Info(msg string, tags ...Field) {
	L().Zap().Info(msg, tags...)
}

// Warnf 输出 warning 级别的日志.
func Warnf(format string, args ...any) {
	L().Sugar().Warnf(format, args...)
}

// Warn 输出 warning 级别的日志.
func Warn(msg string, tags ...Field) {
	L().Zap().Warn(msg, tags...)
}

// Errorf 输出 error 级别的日志.
func Errorf(format string, args ...any) {
	L().Sugar().Errorf(format, args...)
}

// Error 输出 error 级别的日志.
func Error(msg string, tags ...Field) {
	L().Zap().Error(msg, tags...)
}

// Panicf 输出 panic 级别的日志.
func Panicf(format string, args ...any) {
	L().Sugar().Panicf(format, args...)
}

// Panic 输出 panic 级别的日志.
func Panic(msg string, tags ...Field) {
	L().Zap().Panic(msg, tags...)
}

// Fatalf 输出 fatal 级别的日志.
func Fatalf(format string, args ...any) {
	L().Sugar().Fatalf(format, args...)
}

// Fatal 输出 fatal 级别的日志.
func Fatal(msg string, tags ...Field) {
	L().Zap().Fatal(msg, tags...)
}

func With(fields ...Field) Logger {
	return L().With(fields...)
}

func AddCallerSkip(skip int) Logger {
	return L().AddCallerSkip(skip)
}

func W(ctx context.Context) Logger {
	return L().W(ctx)
}

func WC(ctx context.Context, contextExtractors map[string]func(context.Context) string) Logger {
	return L().WC(ctx, contextExtractors)
}
