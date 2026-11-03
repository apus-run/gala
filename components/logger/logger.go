// Package logger provides slog-based logging with global helpers and context attributes | 基于 slog 的日志：全局助手与 context 属性注入.
//
// # Notes | 说明
//
// A thin layer over log/slog, not a framework: the global helpers (Info,
// Error, ...) delegate to slog.Default, and New/NewHandler build standard
// text and JSON handlers plus a pretty format (k0kubun/pp, colors off by
// default). Any slog.Handler remains usable — WrapHandler only injects
// attributes extracted from the context (AttrsFromContext by default).
// Output goes to a plain io.Writer; rotation and shipping are the
// application's job.
// 这是 log/slog 之上的薄层而非框架：全局函数（Info、Error……）代理
// slog.Default；New/NewHandler 构建标准 text/JSON handler 及 pretty 格式
// （k0kubun/pp，默认关闭颜色）。任意 slog.Handler 仍可使用——WrapHandler
// 只注入从 context 提取的属性（默认 AttrsFromContext）。输出是普通
// io.Writer，轮转与采集由应用负责。
//
// # Features | 功能
//
// - Global level helpers mirroring slog signatures | 与 slog 同签名的全局日志函数
// - Text/JSON/Pretty handler construction | text/JSON/pretty 三种 handler
// - Context attribute injection via Extractor | 经 Extractor 注入 context 属性
// - ParseLevel understands fatal and panic | ParseLevel 支持 fatal 与 panic
//
// # Example | 示例
//
//	logger.SetDefault(logger.New(logger.WithFormat(logger.FormatJSON)))
//	ctx = logger.ContextWithAttrs(ctx, slog.String("uid", "42"))
//	logger.InfoContext(ctx, "order paid", "order_id", "o-1")
package logger

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// SetDefault makes l the package-global logger | SetDefault 将 l 设为包级默认日志器。
func SetDefault(l *slog.Logger) {
	slog.SetDefault(l)
}

// Default returns the global logger | Default 返回全局日志器。
func Default() *slog.Logger {
	return slog.Default()
}

// L is a shorthand for Default | L 是 Default 的简写。
func L() *slog.Logger {
	return slog.Default()
}

// With returns the default logger with args attached | With 返回附加 args 的默认日志器。
func With(args ...any) *slog.Logger {
	return Default().With(args...)
}

// WithGroup returns the default logger under a named group
// | WithGroup 返回挂在指定分组下的默认日志器。
func WithGroup(name string) *slog.Logger {
	return Default().WithGroup(name)
}

// Handler returns the default logger's handler | Handler 返回默认日志器的 handler。
func Handler() slog.Handler {
	return Default().Handler()
}

// Enabled reports whether the level is on for ctx | Enabled 判断 ctx 下该级别是否启用。
func Enabled(ctx context.Context, level Level) bool {
	return Default().Enabled(ctx, level)
}

// Debug logs at debug level using the global logger | Debug 用全局日志器输出 debug 级别日志。
func Debug(msg string, args ...any) {
	log(context.Background(), LevelDebug, msg, args...)
}

// DebugContext logs at debug level carrying ctx | DebugContext 携带 ctx 输出 debug 级别日志。
func DebugContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelDebug, msg, args...)
}

// Info logs at info level using the global logger | Info 用全局日志器输出 info 级别日志。
func Info(msg string, args ...any) {
	log(context.Background(), LevelInfo, msg, args...)
}

// InfoContext logs at info level carrying ctx | InfoContext 携带 ctx 输出 info 级别日志。
func InfoContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelInfo, msg, args...)
}

// Warn logs at warn level using the global logger | Warn 用全局日志器输出 warn 级别日志。
func Warn(msg string, args ...any) {
	log(context.Background(), LevelWarn, msg, args...)
}

// WarnContext logs at warn level carrying ctx | WarnContext 携带 ctx 输出 warn 级别日志。
func WarnContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelWarn, msg, args...)
}

// Error logs at error level using the global logger | Error 用全局日志器输出 error 级别日志。
func Error(msg string, args ...any) {
	log(context.Background(), LevelError, msg, args...)
}

// ErrorContext logs at error level carrying ctx | ErrorContext 携带 ctx 输出 error 级别日志。
func ErrorContext(ctx context.Context, msg string, args ...any) {
	log(ctx, LevelError, msg, args...)
}

// Log logs at the given level carrying ctx | Log 携带 ctx 按指定级别输出日志。
func Log(ctx context.Context, level Level, msg string, args ...any) {
	log(ctx, level, msg, args...)
}

// LogAttrs logs at the given level with typed attrs | LogAttrs 以类型化属性按指定级别输出日志。
func LogAttrs(ctx context.Context, level Level, msg string, attrs ...slog.Attr) {
	handler := Default().Handler()
	if !handler.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.AddAttrs(attrs...)
	_ = handler.Handle(ctx, record)
}

func log(ctx context.Context, level Level, msg string, args ...any) {
	handler := Default().Handler()
	if !handler.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.Add(args...)
	_ = handler.Handle(ctx, record)
}
