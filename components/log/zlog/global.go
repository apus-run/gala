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
// SetLogger 替换全局日志器，可安全并发使用；传入 nil 会安装一个新的默认日志器。
// 具体类型必须保持为 *ZapLogger：初始化时安装的就是该类型，atomic.Value 也要求类型一致。
func SetLogger(logger Logger) {
	if logger == nil {
		logger = NewLogger()
	}
	global.Store(logger)
}

// GetLogger returns the current global logger. It is safe for concurrent use. | 返回当前全局日志器，可安全并发使用。
func GetLogger() Logger {
	return L()
}

// L is a shorthand for GetLogger | GetLogger 的简写。
func L() Logger {
	return global.Load().(Logger)
}

// Default is the same as GetLogger | 等价于 GetLogger。
func Default() Logger {
	return L()
}

// Debugf logs a message at debug level | 输出 debug 级别的日志.
func Debugf(format string, args ...any) {
	L().Sugar().Debugf(format, args...)
}

// Debug logs a message at debug level | 输出 debug 级别的日志.
func Debug(msg string, tags ...Field) {
	L().Zap().Debug(msg, tags...)
}

// Infof logs a message at info level | 输出 info 级别的日志.
func Infof(format string, args ...any) {
	L().Sugar().Infof(format, args...)
}

// Info logs a message at info level | 输出 info 级别的日志.
func Info(msg string, tags ...Field) {
	L().Zap().Info(msg, tags...)
}

// Warnf logs a message at warning level | 输出 warning 级别的日志.
func Warnf(format string, args ...any) {
	L().Sugar().Warnf(format, args...)
}

// Warn logs a message at warning level | 输出 warning 级别的日志.
func Warn(msg string, tags ...Field) {
	L().Zap().Warn(msg, tags...)
}

// Errorf logs a message at error level | 输出 error 级别的日志.
func Errorf(format string, args ...any) {
	L().Sugar().Errorf(format, args...)
}

// Error logs a message at error level | 输出 error 级别的日志.
func Error(msg string, tags ...Field) {
	L().Zap().Error(msg, tags...)
}

// Panicf logs a message at panic level | 输出 panic 级别的日志.
func Panicf(format string, args ...any) {
	L().Sugar().Panicf(format, args...)
}

// Panic logs a message at panic level | 输出 panic 级别的日志.
func Panic(msg string, tags ...Field) {
	L().Zap().Panic(msg, tags...)
}

// Fatalf logs a message at fatal level | 输出 fatal 级别的日志.
func Fatalf(format string, args ...any) {
	L().Sugar().Fatalf(format, args...)
}

// Fatal logs a message at fatal level | 输出 fatal 级别的日志.
func Fatal(msg string, tags ...Field) {
	L().Zap().Fatal(msg, tags...)
}

// With returns the global logger with the given fields attached | 返回附加了给定字段的全局日志器.
func With(fields ...Field) Logger {
	return L().With(fields...)
}

// AddCallerSkip returns the global logger skipping skip more caller frames | 返回多跳过 skip 层调用帧的全局日志器.
func AddCallerSkip(skip int) Logger {
	return L().AddCallerSkip(skip)
}

// W returns the global logger carrying the request/trace/span IDs found in ctx | 返回携带 ctx 中请求/跟踪/跨度 ID 的全局日志器.
func W(ctx context.Context) Logger {
	return L().W(ctx)
}

// WC returns the global logger with values extracted from ctx via the extractors | 返回按 extractors 从 ctx 提取值并附加的全局日志器.
func WC(ctx context.Context, contextExtractors map[string]func(context.Context) string) Logger {
	return L().WC(ctx, contextExtractors)
}
