package zlog

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// contextLogKey is how we find Loggers in a context.Context. | 用于在 context.Context 中查找 Logger 的键。
type contextLogKey struct{}

// WithContext returns a copy of context in which the log value is set. | 返回设置了日志值的 context 副本。
func WithContext(ctx context.Context, keyvals ...any) context.Context {
	return FromContext(ctx).WithContext(ctx, keyvals...)
}

// WithContext returns a context carrying this logger plus the even-length keyvals as fields | 返回携带本日志器的 context，并将成对的键值作为字段附加.
func (l *ZapLogger) WithContext(ctx context.Context, keyvals ...any) context.Context {
	with := func(l Logger) context.Context {
		return context.WithValue(ctx, contextLogKey{}, l)
	}

	keylen := len(keyvals)
	if keylen == 0 || keylen%2 != 0 {
		return with(l)
	}

	data := make([]zap.Field, 0, (keylen/2)+1)
	for i := 0; i < keylen; i += 2 {
		data = append(data, zap.Any(fmt.Sprint(keyvals[i]), keyvals[i+1]))
	}

	return with(l.With(data...))
}

// FromContext returns a logger with predefined values from a context.Context. | 返回从 context.Context 中取出并带有预置字段的日志器。
func FromContext(ctx context.Context, keyvals ...any) Logger {
	var log Logger = L()
	if ctx != nil {
		if logger, ok := ctx.Value(contextLogKey{}).(Logger); ok {
			log = logger
		}
	}

	keylen := len(keyvals)
	if keylen == 0 || keylen%2 != 0 {
		return log
	}

	data := make([]zap.Field, 0, (keylen/2)+1)
	for i := 0; i < keylen; i += 2 {
		data = append(data, zap.Any(fmt.Sprint(keyvals[i]), keyvals[i+1]))
	}

	return log.With(data...)
}

// C represents for `FromContext` with empty keyvals. | 等价于不带键值对的 FromContext。
func C(ctx context.Context) Logger {
	return FromContext(ctx)
}

// Keys defined for context values | 定义用于上下文的键.
type (
	// requestIDKey is the context key for the request ID | 定义请求 ID 的上下文键.
	requestIDKey struct{}
	// userIDKey is the context key for the user ID | 定义用户 ID 的上下文键.
	userIDKey struct{}
	// userNameKey is the context key for the username | 定义用户名的上下文键.
	userNameKey struct{}

	// traceIDKey is the context key for the trace ID | 定义跟踪 ID 的上下文键.
	traceIDKey struct{}
	// spanIDKey is the context key for the span ID | 定义跨度 ID 的上下文键.
	spanIDKey struct{}
)

// WithRequestID stores the request ID in the context | 将请求 ID 存入 context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestIDFromContext returns the request ID, or "" if absent | 返回请求 ID，缺失时返回空串.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if requestID, ok := ctx.Value(requestIDKey{}).(string); ok {
		return requestID
	}
	return ""
}

// WithUserID stores the user ID in the context | 将用户 ID 存入 context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserIDFromContext returns the user ID, or "" if absent | 返回用户 ID，缺失时返回空串.
func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if userID, ok := ctx.Value(userIDKey{}).(string); ok {
		return userID
	}
	return ""
}

// WithUsername stores the username in the context | 将用户名存入 context.
func WithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, userNameKey{}, username)
}

// UsernameFromContext returns the username, or "" if absent | 返回用户名，缺失时返回空串.
func UsernameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if username, ok := ctx.Value(userNameKey{}).(string); ok {
		return username
	}
	return ""
}

// WithTraceID stores the trace ID in the context | 将跟踪 ID 存入 context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext returns the trace ID, or "" if absent | 返回跟踪 ID，缺失时返回空串.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if traceID, ok := ctx.Value(traceIDKey{}).(string); ok {
		return traceID
	}
	return ""
}

// WithSpanID stores the span ID in the context | 将跨度 ID 存入 context.
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, spanIDKey{}, spanID)
}

// SpanIDFromContext returns the span ID, or "" if absent | 返回跨度 ID，缺失时返回空串.
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if spanID, ok := ctx.Value(spanIDKey{}).(string); ok {
		return spanID
	}
	return ""
}
