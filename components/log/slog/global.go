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
// SetLogger 替换全局日志器，可安全并发使用；传入 nil 会安装一个新的默认日志器。
// 具体类型必须保持为 *SlogLogger：初始化时安装的就是该类型，atomic.Value 也要求类型一致。
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

// Info logs at Info level via the global logger | 通过全局日志器以 Info 级别记录日志.
func Info(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelInfo, msg, args...)
}

// Infof logs a formatted message at Info level via the global logger | 通过全局日志器以 Info 级别记录格式化日志.
func Infof(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelInfo, sprintf(format, args...))
}

// InfoContext logs at Info level with the given context | 携带给定 context 以 Info 级别记录日志.
func InfoContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelInfo, msg, args...)
}

// Error logs at Error level via the global logger | 通过全局日志器以 Error 级别记录日志.
func Error(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelError, msg, args...)
}

// Errorf logs a formatted message at Error level via the global logger | 通过全局日志器以 Error 级别记录格式化日志.
func Errorf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelError, sprintf(format, args...))
}

// ErrorContext logs at Error level with the given context | 携带给定 context 以 Error 级别记录日志.
func ErrorContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelError, msg, args...)
}

// Debug logs at Debug level via the global logger | 通过全局日志器以 Debug 级别记录日志.
func Debug(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelDebug, msg, args...)
}

// Debugf logs a formatted message at Debug level via the global logger | 通过全局日志器以 Debug 级别记录格式化日志.
func Debugf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelDebug, sprintf(format, args...))
}

// DebugContext logs at Debug level with the given context | 携带给定 context 以 Debug 级别记录日志.
func DebugContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelDebug, msg, args...)
}

// Warn logs at Warn level via the global logger | 通过全局日志器以 Warn 级别记录日志.
func Warn(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelWarn, msg, args...)
}

// Warnf logs a formatted message at Warn level via the global logger | 通过全局日志器以 Warn 级别记录格式化日志.
func Warnf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelWarn, sprintf(format, args...))
}

// WarnContext logs at Warn level with the given context | 携带给定 context 以 Warn 级别记录日志.
func WarnContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelWarn, msg, args...)
}

// Fatal logs at Fatal level and exits the process | 以 Fatal 级别记录日志并退出进程.
func Fatal(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelFatal, msg, args...)
	os.Exit(1)
}

// Fatalf logs a formatted message at Fatal level and exits the process | 以 Fatal 级别记录格式化日志并退出进程.
func Fatalf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelFatal, sprintf(format, args...))
	os.Exit(1)
}

// FatalContext logs at Fatal level with the given context and exits the process | 携带给定 context 以 Fatal 级别记录日志并退出进程.
func FatalContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelFatal, msg, args...)
	os.Exit(1)
}

// Panic logs at Panic level and panics | 以 Panic 级别记录日志并触发 panic.
func Panic(msg string, args ...Attr) {
	logAttrs(L(), context.Background(), LevelPanic, msg, args...)
	panic(SprintfWithAttrs(msg, args...))
}

// Panicf logs a formatted message at Panic level and panics | 以 Panic 级别记录格式化日志并触发 panic.
func Panicf(format string, args ...any) {
	logAttrs(L(), context.Background(), LevelPanic, sprintf(format, args...))
	panic(fmt.Sprintf(format, args...))
}

// PanicContext logs at Panic level with the given context and panics | 携带给定 context 以 Panic 级别记录日志并触发 panic.
func PanicContext(ctx context.Context, msg string, args ...Attr) {
	logAttrs(L(), ctx, LevelPanic, msg, args...)
	panic(SprintfWithAttrs(msg, args...))
}

// With returns the global logger with the given args attached | 返回附加了给定 args 的全局日志器.
func With(args ...any) *SlogLogger {
	return L().With(args...)
}

// WithGroup returns the global logger under the named group | 返回置于指定分组下的全局日志器.
func WithGroup(name string) *SlogLogger {
	return L().WithGroup(name)
}
