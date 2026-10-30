// https://tonybai.com/2023/09/01/slog-a-new-choice-for-logging-in-go/

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

var defaultHandler atomic.Pointer[Handler]

const LOGGER_KEY = "slogLogger"

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
	LevelPanic = slog.Level(10)
	LevelFatal = slog.Level(12)
)

var LevelNames = map[slog.Leveler]string{
	LevelFatal: "FATAL",
	LevelError: "ERROR",
	LevelWarn:  "WARN",
	LevelInfo:  "INFO",
	LevelDebug: "DEBUG",
	LevelPanic: "PANIC",
}

type SlogLogger struct {
	*slog.Logger
}

// NewSlogLogger 只包装了 slog
func NewSlogLogger(l *slog.Logger) Logger {
	if l == nil {
		l = slog.Default()
	}
	return &SlogLogger{l}
}

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
	// 日志级别
	level := getLogLevel(options.LogLevel)

	handlerOptions := &slog.HandlerOptions{
		Level:       level,
		AddSource:   true,
		ReplaceAttr: ReplaceAttr,
	}

	switch f := options.Format; f {
	case FormatText:
		handler = slog.NewTextHandler(os.Stdout, handlerOptions)
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

// NewNop returns a no-op logger
func NewNop() *slog.Logger {
	nopLevel := slog.Level(-99)
	ops := &slog.HandlerOptions{
		Level: nopLevel,
	}
	handler := slog.NewTextHandler(io.Discard, ops)
	return slog.New(handler)
}

// NewWithHandler build *slog.Logger with slog Handler
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

func ApplyHandlerOption(opt HandlerOption) {
	defaultHandler.Load().Apply(opt)
}

const AttrErrorKey = "error"

// ReplaceAttr handle log key-value pair
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

// Log send log records with caller depth
func (l *SlogLogger) Log(ctx context.Context, depth int, err error, level slog.Level, msg string, attrs ...any) {
	if !l.Enabled(ctx, level) {
		return
	}

	// 记录日志
	l.Logger.Log(ctx, level, msg, attrs...)
}

// logAttrs is called directly by one public logging method or package function.
// Both paths have the same depth, so caller lookup needs no stack-name detection.
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
	runtime.Callers(3, pcs[:])
	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.AddAttrs(attrs...)
	_ = handler.Handle(ctx, record)
}

// 实现 Logger 接口的方法
func (l *SlogLogger) Info(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelInfo, msg, attrs...)
}

func (l *SlogLogger) Infof(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelInfo, sprintf(msg, args...))
}

func (l *SlogLogger) InfoContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelInfo, msg, attrs...)
}

func (l *SlogLogger) Error(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelError, msg, attrs...)
}

func (l *SlogLogger) Errorf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelError, sprintf(msg, args...))
}

func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelError, msg, attrs...)
}

func (l *SlogLogger) Debug(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelDebug, msg, attrs...)
}

func (l *SlogLogger) Debugf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelDebug, sprintf(msg, args...))
}

func (l *SlogLogger) DebugContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelDebug, msg, attrs...)
}

func (l *SlogLogger) Warn(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelWarn, msg, attrs...)
}

func (l *SlogLogger) Warnf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelWarn, sprintf(msg, args...))
}

func (l *SlogLogger) WarnContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelWarn, msg, attrs...)
}

func (l *SlogLogger) Fatal(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelFatal, msg, attrs...)
	os.Exit(1)
}

func (l *SlogLogger) FatalContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelFatal, msg, attrs...)
	os.Exit(1)
}

func (l *SlogLogger) Fatalf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelFatal, sprintf(msg, args...))
	os.Exit(1)
}

func (l *SlogLogger) Panic(msg string, attrs ...Attr) {
	logAttrs(l, context.Background(), LevelPanic, msg, attrs...)
	panic(SprintfWithAttrs(msg, attrs...))
}

func (l *SlogLogger) PanicContext(ctx context.Context, msg string, attrs ...Attr) {
	logAttrs(l, ctx, LevelPanic, msg, attrs...)
	panic(SprintfWithAttrs(msg, attrs...))
}

func (l *SlogLogger) Panicf(msg string, args ...any) {
	logAttrs(l, context.Background(), LevelPanic, sprintf(msg, args...))
	panic(fmt.Sprintf(msg, args...))
}

func SprintfWithAttrs(format string, attrs ...Attr) string {
	// 处理 Attr 类型并将其转换为字符串
	var attrStr strings.Builder
	for _, attr := range attrs {
		attrStr.WriteString(fmt.Sprintf("%v ", attr)) // 根据需要格式化 Attr
	}
	return fmt.Sprintf(format, attrStr.String())
}

func (l *SlogLogger) With(args ...any) *SlogLogger {
	lc := l.clone()
	lc.Logger = lc.Logger.With(args...)
	return lc
}

func (l *SlogLogger) WithGroup(name string) *SlogLogger {
	lc := l.clone()
	lc.Logger = lc.Logger.WithGroup(name)
	return lc
}

// clone 深度拷贝 SlogLogger.
func (l *SlogLogger) clone() *SlogLogger {
	copied := *l
	return &copied
}

func (l *SlogLogger) W(ctx context.Context) Logger {
	contextExtractors := map[string]func(context.Context) string{
		"x-request-id": RequestIDFromContext, // 从 context 中提取请求 ID
		"x-trace-id":   TraceIDFromContext,   // 从 context 中提取跟踪 ID
		"x-span-id":    SpanIDFromContext,    // 从 context 中提取跨度 ID
	}

	return l.WC(ctx, contextExtractors)
}

// WC 解析传入的 context，尝试提取关注的键值，并添加到 zap.Logger 结构化日志中.
func (l *SlogLogger) WC(ctx context.Context, contextExtractors map[string]func(context.Context) string) Logger {
	lc := l.clone()

	// 遍历映射，从 context 中提取值并添加到日志中。
	for fieldName, extractor := range contextExtractors {
		if val := extractor(ctx); val != "" {
			lc.Logger = lc.Logger.With(slog.String(fieldName, val))
		}
	}

	return lc
}
