package slog

import (
	"context"
)

type contextLogKey struct{}

// NewContext returns a context carrying the given logger; a nil logger leaves ctx unchanged | 返回携带给定日志器的 context；logger 为 nil 时原样返回 ctx.
func NewContext(ctx context.Context, l *SlogLogger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, contextLogKey{}, l)
}

// WithContext stores the logger only if the context has none | 仅当 context 中尚无日志器时才存入.
func WithContext(ctx context.Context, l *SlogLogger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil {
		return ctx
	}
	if _, ok := ctx.Value(contextLogKey{}).(*SlogLogger); ok {
		return ctx
	}
	return context.WithValue(ctx, contextLogKey{}, l)
}

// FromContext returns the logger in ctx, falling back to the global one | 返回 ctx 中的日志器，缺失时回退到全局日志器.
func FromContext(ctx context.Context) *SlogLogger {
	if ctx != nil {
		if l, ok := ctx.Value(contextLogKey{}).(*SlogLogger); ok {
			return l
		}
	}
	if l, ok := L().(*SlogLogger); ok {
		return l
	}
	return nil
}

// C represents for `FromContext` with empty keyvals.
// slog.C(ctx).Info("Set function called")
// C 等价于不带键值对的 FromContext。
func C(ctx context.Context) *SlogLogger {
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
