// Package zlog provides a logger built on go.uber.org/zap, with global helpers and context support | 基于 go.uber.org/zap 的日志封装，提供全局日志函数与上下文支持。
package zlog

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LOGGER_KEY is the context key name used to carry a logger | 携带日志器所用的上下文键名.
const LOGGER_KEY = "zapLogger"

var _ Logger = (*ZapLogger)(nil)

// ZapLogger is the package's Logger implementation wrapping *zap.Logger | 包装 *zap.Logger 的 Logger 实现.
type ZapLogger struct {
	*zap.Logger
}

// NewZapLogger only wraps zap | 只是对 zap 的简单包装。
func NewZapLogger(l *zap.Logger) Logger {
	return &ZapLogger{
		l,
	}
}

// Zap returns the underlying zap logger without changing its options. | 返回底层 zap 日志器，不改变其配置。
func (l *ZapLogger) Zap() *zap.Logger {
	return l.Logger
}

// ZapDevConfig returns zap's development config | 返回 zap 的开发环境配置.
func ZapDevConfig() zap.Config {
	return zap.NewDevelopmentConfig()
}

// ZapProdConfig returns zap's production config | 返回 zap 的生产环境配置.
func ZapProdConfig() zap.Config {
	return zap.NewProductionConfig()
}

// NewLogger wraps zap with log file rotation and archival | 包装 zap 并支持日志文件切割归档。
func NewLogger(opts ...Option) *ZapLogger {
	options := NewOptions(opts...)

	// Encoder config.
	// 编码器配置
	encoder := getEncoder(options)

	// Log level.
	// 日志级别
	level := getLogLevel(options.LogLevel)

	core := zapcore.NewCore(encoder, options.Writer, level)

	return &ZapLogger{
		buildZapLogger(core, options.Mode),
	}
}

// buildZapLogger wraps the core build logic | 封装核心构建逻辑。
func buildZapLogger(core zapcore.Core, mode string) *zap.Logger {
	baseOpts := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(1), // skip the wrapper frame | 跳过封装层
		zap.AddStacktrace(zap.ErrorLevel),
	}

	if mode != "prod" {
		return zap.New(core, append(baseOpts, zap.Development())...)
	}
	return zap.New(core, baseOpts...)
}

func getEncoder(options *Options) zapcore.Encoder {
	// Create a different encoder for each log format.
	// 根据不同的日志格式创建不同的编码器
	switch options.Format {
	case FormatJSON:
		return zapcore.NewJSONEncoder(getJSONEncoderConfig())
	case FormatText:
		return zapcore.NewConsoleEncoder(getConsoleEncoderConfig())
	default:
		return zapcore.NewConsoleEncoder(getConsoleEncoderConfig())
	}
}

// getConsoleEncoderConfig returns a dev-friendly console encoder config | 开发友好的控制台编码配置。
func getConsoleEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "ts",
		NameKey:        "logger",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // record the level in capital letters in log files | 在日志文件中使用大写字母记录日志级别
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.FullCallerEncoder,
	}
}

// getJSONEncoderConfig returns the JSON encoder config for production | 生产环境 JSON 编码配置。
func getJSONEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "timestamp",
		NameKey:        "logger",
		CallerKey:      "caller",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     customTimeEncoder,             // custom time format | 自定义时间格式
		EncodeDuration: zapcore.MillisDurationEncoder, // millisecond precision | 毫秒精度
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}

// customTimeEncoder formats time with a custom layout | 自定义时间格式。
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	// Serialize the time as `2006-01-02 15:04:05.000`, which is easier to read.
	// 指定时间序列化函数，将时间序列化为 `2006-01-02 15:04:05.000` 格式，更易读
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

func getLogLevel(logLevel string) zapcore.Level {
	if logLevel == "" {
		return zapcore.InfoLevel // default level | 默认级别
	}
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(logLevel)); err != nil {
		// Fall back to error level when the given log level is invalid.
		// 如果指定了非法的日志级别，则默认使用 error 级别
		return zap.ErrorLevel
	}

	return zapLevel
}

// Logging methods retain one wrapper frame to match buildZapLogger's caller skip.
// Sugar and Sync need no wrappers because their native methods are promoted.
// 这些日志方法保留一层包装帧，以配合 buildZapLogger 的调用方跳过数。
// Sugar 和 Sync 无需包装，因为其原生方法已被直接提升。
// Info logs a message at info level | 输出 info 级别的日志.
func (l *ZapLogger) Info(msg string, tags ...Field) {
	l.Logger.Info(msg, tags...)
}

// Error logs a message at error level | 输出 error 级别的日志.
func (l *ZapLogger) Error(msg string, tags ...Field) {
	l.Logger.Error(msg, tags...)
}

// Debug logs a message at debug level | 输出 debug 级别的日志.
func (l *ZapLogger) Debug(msg string, tags ...Field) {
	l.Logger.Debug(msg, tags...)
}

// Warn logs a message at warning level | 输出 warning 级别的日志.
func (l *ZapLogger) Warn(msg string, tags ...Field) {
	l.Logger.Warn(msg, tags...)
}

// Fatal logs a message at fatal level | 输出 fatal 级别的日志.
func (l *ZapLogger) Fatal(msg string, tags ...Field) {
	l.Logger.Fatal(msg, tags...)
}

// Panic logs a message at panic level | 输出 panic 级别的日志.
func (l *ZapLogger) Panic(msg string, tags ...Field) {
	l.Logger.Panic(msg, tags...)
}

// Slow logs a message as a slow-operation warning | 以慢操作告警的形式输出日志.
func (l *ZapLogger) Slow(msg string, tags ...Field) {
	l.Logger.Warn(msg, tags...)
}

// Stack logs an error message with the current goroutine stack | 输出附带当前 goroutine 堆栈的错误日志.
func (l *ZapLogger) Stack(msg string) {
	l.Logger.Error(msg, zap.Stack("stack"))
}

// Stat logs a statistics message at info level | 以 info 级别输出统计信息日志.
func (l *ZapLogger) Stat(msg string, tags ...Field) {
	l.Logger.Info(msg, tags...)
}

// Debugf logs a formatted message at debug level | 输出 debug 级别的格式化日志.
func (l *ZapLogger) Debugf(format string, args ...any) {
	l.Logger.Sugar().Debugf(format, args...)
}

// Infof logs a formatted message at info level | 输出 info 级别的格式化日志.
func (l *ZapLogger) Infof(format string, args ...any) {
	l.Logger.Sugar().Infof(format, args...)
}

// Warnf logs a formatted message at warning level | 输出 warning 级别的格式化日志.
func (l *ZapLogger) Warnf(format string, args ...any) {
	l.Logger.Sugar().Warnf(format, args...)
}

// Errorf logs a formatted message at error level | 输出 error 级别的格式化日志.
func (l *ZapLogger) Errorf(format string, args ...any) {
	l.Logger.Sugar().Errorf(format, args...)
}

// Fatalf logs a formatted message at fatal level | 输出 fatal 级别的格式化日志.
func (l *ZapLogger) Fatalf(format string, args ...any) {
	l.Logger.Sugar().Fatalf(format, args...)
}

// Panicf logs a formatted message at panic level | 输出 panic 级别的格式化日志.
func (l *ZapLogger) Panicf(format string, args ...any) {
	l.Logger.Sugar().Panicf(format, args...)
}

// Print logs args as a single info-level message | 将 args 拼接后以 info 级别输出.
func (l *ZapLogger) Print(args ...any) {
	l.Logger.Info(fmt.Sprint(args...))
}

// Printf logs a formatted message at info level | 输出 info 级别的格式化日志.
func (l *ZapLogger) Printf(format string, args ...any) {
	l.Logger.Sugar().Infof(format, args...)
}

// Close is compatible with the io.Closer interface | 兼容关闭接口。
func (l *ZapLogger) Close() error {
	return l.Sync()
}

// With creates a child logger and adds structured context to it. Fields added
// to the child don't affect the parent, and vice versa.
// With 创建子日志器并添加结构化上下文；子日志器添加的字段不影响父日志器，反之亦然。
func (l *ZapLogger) With(fields ...Field) Logger {
	if len(fields) == 0 {
		return l
	}

	lc := l.clone()
	lc.Logger = lc.Logger.With(fields...)
	return lc
}

// AddCallerSkip increases the number of callers skipped by caller annotation
// (as enabled by the AddCaller option). When building wrappers around the
// Logger and SugaredLogger, supplying this Option prevents zap from always
// reporting the wrapper code as the caller.
// AddCallerSkip 增加调用方标注（由 AddCaller 选项启用）跳过的调用方数量。
// 在 Logger 和 SugaredLogger 外面包装时，提供该选项可避免 zap 总是把包装代码报告为调用方。
func (l *ZapLogger) AddCallerSkip(skip int) Logger {
	lc := l.clone()
	lc.Logger = lc.Logger.WithOptions(zap.AddCallerSkip(skip))
	return lc
}

// clone shallow-copies the wrapper; the underlying zap logger is derived via With/WithOptions.
// clone 浅拷贝包装器；底层 zap logger 通过 With/WithOptions 派生。
func (l *ZapLogger) clone() *ZapLogger {
	copied := *l
	return &copied
}

// W returns a logger carrying the request/trace/span IDs found in ctx under their x-*-id keys | 返回携带 ctx 中以 x-*-id 为键的请求/跟踪/跨度 ID 的日志器.
func (l *ZapLogger) W(ctx context.Context) Logger {
	contextExtractors := map[string]func(context.Context) string{
		"x-request-id": RequestIDFromContext, // extract the request ID from the context | 从 context 中提取请求 ID
		"x-trace-id":   TraceIDFromContext,   // extract the trace ID from the context | 从 context 中提取跟踪 ID
		"x-span-id":    SpanIDFromContext,    // extract the span ID from the context | 从 context 中提取跨度 ID
	}

	return l.WC(ctx, contextExtractors)
}

// WC parses the given context, extracts the interested key-values, and adds them to the structured zap log.
// WC 解析传入的 context，尝试提取关注的键值，并添加到 zap.Logger 结构化日志中.
func (l *ZapLogger) WC(ctx context.Context, contextExtractors map[string]func(context.Context) string) Logger {
	lc := l.clone()

	// Iterate the map, extract values from the context, and add them to the log.
	// 遍历映射，从 context 中提取值并添加到日志中。
	for k, extractor := range contextExtractors {
		if v := extractor(ctx); v != "" {
			lc.Logger = lc.Logger.With(zap.String(k, v))
		}
	}

	return lc
}
