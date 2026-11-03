// https://tonybai.com/2023/09/01/slog-a-new-choice-for-logging-in-go/

// Package slog provides a logger built on the standard library log/slog, with global helpers and context support | 基于标准库 log/slog 的日志封装，提供全局日志函数与上下文支持。
package slog

import (
	"context"
	"fmt"
	"go/build"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

var _ Logger = (*SlogLogger)(nil)

// defaultHandler holds the handler installed by the most recent NewLogger | 保存最近一次 NewLogger 安装的 handler.
var defaultHandler atomic.Pointer[Handler]

// LOGGER_KEY is the context key name used to carry a logger | 携带日志器所用的上下文键名.
const LOGGER_KEY = "slogLogger"

// Log levels accepted by this package | 本包接受的日志级别.
const (
	// LevelDebug is slog's debug level | 对应 slog 的 debug 级别.
	LevelDebug = slog.LevelDebug
	// LevelInfo is slog's info level | 对应 slog 的 info 级别.
	LevelInfo = slog.LevelInfo
	// LevelWarn is slog's warn level | 对应 slog 的 warn 级别.
	LevelWarn = slog.LevelWarn
	// LevelError is slog's error level | 对应 slog 的 error 级别.
	LevelError = slog.LevelError
	// LevelPanic is a custom level above error that panics after logging | 高于 error 的自定义级别，记录后触发 panic.
	LevelPanic = slog.Level(10)
	// LevelFatal is a custom level above panic that exits after logging | 高于 panic 的自定义级别，记录后退出进程.
	LevelFatal = slog.Level(12)
)

// LevelNames maps log levels to their display names | 日志级别到显示名称的映射.
var LevelNames = map[slog.Leveler]string{
	LevelFatal: "FATAL",
	LevelError: "ERROR",
	LevelWarn:  "WARN",
	LevelInfo:  "INFO",
	LevelDebug: "DEBUG",
	LevelPanic: "PANIC",
}

// SlogLogger is the package's Logger implementation wrapping *slog.Logger | 包装 *slog.Logger 的 Logger 实现.
type SlogLogger struct {
	*slog.Logger
}

// NewSlogLogger only wraps slog | 只是对 slog 的简单包装。
func NewSlogLogger(l *slog.Logger) Logger {
	if l == nil {
		l = slog.Default()
	}
	return &SlogLogger{l}
}

// NewLogger builds a *SlogLogger from opts, installing the resulting handler as the default | 用 opts 构建 *SlogLogger，并将生成的 handler 设为默认.
func NewLogger(opts ...Option) *SlogLogger {
	options := NewOptions(opts...)

	handler := createHandler(options)
	logger := slog.New(handler)

	return &SlogLogger{
		Logger: logger,
	}
}

func createHandler(options *Options) slog.Handler {
	var handler slog.Handler
	// Log level.
	// 日志级别
	level := getLogLevel(options.LogLevel)

	handlerOptions := &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: ReplaceAttr,
	}

	switch f := options.Format; f {
	case FormatText:
		handler = slog.NewTextHandler(options.Writer, handlerOptions)
	case FormatJSON:
		handler = slog.NewJSONHandler(options.Writer, handlerOptions)
	default:
		handler = slog.NewJSONHandler(options.Writer, handlerOptions)
	}

	if options.LogGroup != "" {
		handler = handler.WithGroup(options.LogGroup)
	}
	if len(options.LogAttrs) > 0 {
		handler = handler.WithAttrs(options.LogAttrs)
	}
	wrapped := NewHandler(handler).(*Handler)
	defaultHandler.Store(wrapped)
	return wrapped
}

// NewNop returns a no-op logger | 返回一个空操作（no-op）日志器。
func NewNop() *slog.Logger {
	nopLevel := slog.Level(-99)
	ops := &slog.HandlerOptions{
		Level: nopLevel,
	}
	handler := slog.NewTextHandler(io.Discard, ops)
	return slog.New(handler)
}

// NewWithHandler build *slog.Logger with slog Handler | 用 slog Handler 构建 *slog.Logger。
func NewWithHandler(handler slog.Handler) *slog.Logger {
	return slog.New(handler)
}

func getLogLevel(logLevel string) slog.Level {
	level := new(slog.Level)
	err := level.UnmarshalText([]byte(logLevel))
	if err != nil {
		return LevelError
	}

	return *level
}

// ApplyHandlerOption applies opt to the most recently created default handler | 将 opt 应用到最近创建的默认 handler.
func ApplyHandlerOption(opt HandlerOption) {
	defaultHandler.Load().Apply(opt)
}

// AttrErrorKey is the attribute key under which error values are logged | 记录错误值所用的属性键.
const AttrErrorKey = "error"

// ReplaceAttr handle log key-value pair | 处理日志键值对。
func ReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		return slog.String(a.Key, a.Value.Time().Format(time.RFC3339))
	case slog.LevelKey:
		return slog.String(a.Key, strings.ToLower(a.Value.String()))
	case slog.SourceKey:
		if v, ok := a.Value.Any().(*slog.Source); ok {
			a.Value = slog.StringValue(fmt.Sprintf("%s:%d", getBriefSource(v.File), v.Line))
		}
		return a
	case AttrErrorKey:
		v, ok := a.Value.Any().(interface {
			StackTrace() errors.StackTrace
		})
		if ok {
			st := v.StackTrace()
			return slog.Any(a.Key, slog.GroupValue(
				slog.String("msg", a.Value.String()),
				slog.Any("stack", errors.StackTrace(st)),
			))
		}
		return a
	}
	return a
}

func projectPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	}
	return ""
}

func getBriefSource(source string) string {
	gp := filepath.ToSlash(build.Default.GOPATH)
	if strings.HasPrefix(source, gp) {
		return strings.TrimPrefix(source, gp+"/pkg/mod/")
	}
	pp := filepath.ToSlash(projectPath())
	return strings.TrimPrefix(source, pp+"/")
}

// Log send log records with caller depth | 按指定的调用深度发送日志记录。
func (l *SlogLogger) Log(ctx context.Context, depth int, err error, level slog.Level, msg string, attrs ...any) {
	if !l.Enabled(ctx, level) {
		return
	}

	// Record the log.
	// 记录日志
	l.Logger.Log(ctx, level, msg, attrs...)
}

// logAttrs is called directly by one public logging method or package function.
// Both paths have the same depth, so caller lookup needs no stack-name detection.
// logAttrs 由公开的日志方法或包级函数直接调用，两条路径的调用深度相同，因此查找调用方时无需栈名检测。
func logAttrs(logger Logger, ctx context.Context, level slog.Level, msg string, attrs ...Attr) {
	if ctx == nil {
		ctx = context.Background()
	}
	handler := logger.Handler()
	if !handler.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	// Skip runtime.Callers, logAttrs, and the public logging entry point.
	// 跳过 runtime.Callers、logAttrs 以及公开的日志入口函数。
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.AddAttrs(attrs...)
	_ = handler.Handle(ctx, record)
}

// Implement the Logger interface methods.
// 实现 Logger 接口的方法
// Info logs at Info level | 以 Info 级别记录日志.
func (l *SlogLogger) Info(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelInfo, msg, attrs...)
}

// Infof logs a formatted message at Info level | 以 Info 级别记录格式化日志.
func (l *SlogLogger) Infof(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelInfo, sprintf(msg, args...))
}

// InfoContext logs at Info level with the given context | 携带给定 context 以 Info 级别记录日志.
func (l *SlogLogger) InfoContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelInfo, msg, attrs...)
}

// Error logs at Error level | 以 Error 级别记录日志.
func (l *SlogLogger) Error(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelError, msg, attrs...)
}

// Errorf logs a formatted message at Error level | 以 Error 级别记录格式化日志.
func (l *SlogLogger) Errorf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelError, sprintf(msg, args...))
}

// ErrorContext logs at Error level with the given context | 携带给定 context 以 Error 级别记录日志.
func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelError, msg, attrs...)
}

// Debug logs at Debug level | 以 Debug 级别记录日志.
func (l *SlogLogger) Debug(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelDebug, msg, attrs...)
}

// Debugf logs a formatted message at Debug level | 以 Debug 级别记录格式化日志.
func (l *SlogLogger) Debugf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelDebug, sprintf(msg, args...))
}

// DebugContext logs at Debug level with the given context | 携带给定 context 以 Debug 级别记录日志.
func (l *SlogLogger) DebugContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelDebug, msg, attrs...)
}

// Warn logs at Warn level | 以 Warn 级别记录日志.
func (l *SlogLogger) Warn(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelWarn, msg, attrs...)
}

// Warnf logs a formatted message at Warn level | 以 Warn 级别记录格式化日志.
func (l *SlogLogger) Warnf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelWarn, sprintf(msg, args...))
}

// WarnContext logs at Warn level with the given context | 携带给定 context 以 Warn 级别记录日志.
func (l *SlogLogger) WarnContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelWarn, msg, attrs...)
}

// Fatal logs at Fatal level and exits the process | 以 Fatal 级别记录日志并退出进程.
func (l *SlogLogger) Fatal(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelFatal, msg, attrs...)
	os.Exit(1)
}

// FatalContext logs at Fatal level with the given context and exits the process | 携带给定 context 以 Fatal 级别记录日志并退出进程.
func (l *SlogLogger) FatalContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelFatal, msg, attrs...)
	os.Exit(1)
}

// Fatalf logs a formatted message at Fatal level and exits the process | 以 Fatal 级别记录格式化日志并退出进程.
func (l *SlogLogger) Fatalf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelFatal, sprintf(msg, args...))
	os.Exit(1)
}

// Panic logs at Panic level and panics | 以 Panic 级别记录日志并触发 panic.
func (l *SlogLogger) Panic(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelPanic, msg, attrs...)
	panic(SprintfWithAttrs(msg, attrs...))
}

// PanicContext logs at Panic level with the given context and panics | 携带给定 context 以 Panic 级别记录日志并触发 panic.
func (l *SlogLogger) PanicContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelPanic, msg, attrs...)
	panic(SprintfWithAttrs(msg, attrs...))
}

// Panicf logs a formatted message at Panic level and panics | 以 Panic 级别记录格式化日志并触发 panic.
func (l *SlogLogger) Panicf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelPanic, sprintf(msg, args...))
	panic(fmt.Sprintf(msg, args...))
}

// SprintfWithAttrs formats the message, appending the attrs rendered as text | 格式化消息，并把 attrs 以文本形式附加在后面.
func SprintfWithAttrs(format string, attrs ...Attr) string {
	// Handle Attr values and convert them into a string.
	// 处理 Attr 类型并将其转换为字符串
	var attrStr strings.Builder
	for _, attr := range attrs {
		attrStr.WriteString(fmt.Sprintf("%v ", attr)) // Format the Attr as needed | 根据需要格式化 Attr
	}
	return fmt.Sprintf(format, attrStr.String())
}

// With returns a clone of the logger with the given args attached | 返回附加了给定 args 的日志器克隆.
func (l *SlogLogger) With(args ...any) *SlogLogger {
	lc := l.clone()
	lc.Logger = lc.Logger.With(args...)
	return lc
}

// WithGroup returns a clone of the logger under the named group | 返回置于指定分组下的日志器克隆.
func (l *SlogLogger) WithGroup(name string) *SlogLogger {
	lc := l.clone()
	lc.Logger = lc.Logger.WithGroup(name)
	return lc
}

// clone deep-copies SlogLogger | 深度拷贝 SlogLogger。
func (l *SlogLogger) clone() *SlogLogger {
	copied := *l
	return &copied
}

// W returns a logger carrying the request/trace/span IDs found in ctx under their x-*-id keys | 返回携带 ctx 中以 x-*-id 为键的请求/跟踪/跨度 ID 的日志器.
func (l *SlogLogger) W(ctx context.Context) Logger {
	contextExtractors := map[string]func(context.Context) string{
		"x-request-id": RequestIDFromContext, // extract the request ID from the context | 从 context 中提取请求 ID
		"x-trace-id":   TraceIDFromContext,   // extract the trace ID from the context | 从 context 中提取跟踪 ID
		"x-span-id":    SpanIDFromContext,    // extract the span ID from the context | 从 context 中提取跨度 ID
	}

	return l.WC(ctx, contextExtractors)
}

// WC parses the given context, extracts the interested key-values, and adds them to the structured log.
// WC 解析传入的 context，尝试提取关注的键值，并添加到 zap.Logger 结构化日志中.
func (l *SlogLogger) WC(ctx context.Context, contextExtractors map[string]func(context.Context) string) Logger {
	lc := l.clone()

	// Iterate the map, extract values from the context, and add them to the log.
	// 遍历映射，从 context 中提取值并添加到日志中。
	for fieldName, extractor := range contextExtractors {
		if val := extractor(ctx); val != "" {
			lc.Logger = lc.Logger.With(slog.String(fieldName, val))
		}
	}

	return lc
}
