package gala

import (
	"context"
	"maps"

	"github.com/apus-run/gala/pkg/ctxkey"
)

// ServiceContextKey carries the current *Service in a context | ServiceContextKey 在 context 中携带当前 *Service.
var ServiceContextKey = ctxkey.NewContextKey[Gala]()

// Standard header keys used to propagate request metadata across
// service boundaries. Callers may use these keys or define their own.
// 跨服务边界传递请求元数据的标准 header key。调用方可用这些 key，也可自定义.
const (
	// HeaderTraceID carries the trace id | 传递 trace id 的 header.
	HeaderTraceID = "x-app-trace-id"
	// HeaderUserID carries the user id | 传递用户 id 的 header.
	HeaderUserID = "x-app-user-id"
	// HeaderColorTag carries the color tag | 传递 color tag 的 header.
	HeaderColorTag = "x-app-color-tag"
)

// RequestMetadata is a simple string-keyed map carried through the context chain.
// It is the vehicle for trace IDs, user IDs, color tags and other
// request-scoped attributes.
// RequestMetadata 是随 context 链传递的简单字符串 map，承载 trace ID、user ID、
// color tag 等请求级属性.
type RequestMetadata map[string]string

// RequestMetadataKey stores request-scoped metadata in a context | RequestMetadataKey 在 context 中存取请求级元数据.
var RequestMetadataKey = ctxkey.NewContextKey[RequestMetadata]()

// Clone returns a deep copy of md. It returns nil when md is nil so that an
// absent metadata value round-trips cleanly.
// Clone 返回 md 的深拷贝；md 为 nil 时返回 nil，使“无元数据”这一状态可完整往返.
func (m RequestMetadata) Clone() RequestMetadata {
	return cloneRequestMetadata(m)
}

// GetRequestMetadata returns the value stored under key, or "" if ctx carries
// no request metadata or the key is absent. It never exposes the underlying map.
// GetRequestMetadata 返回 key 存的值；ctx 没有元数据或 key 不存在时返回空串，
// 不会暴露底层 map.
func GetRequestMetadata(ctx context.Context, key string) string {
	md, ok := RequestMetadataKey.FromContext(ctx)
	if !ok {
		return ""
	}
	return md[key]
}

// WithRequestMetadataValue returns a copy of ctx whose request metadata has key
// set to value.
//
// The metadata already in ctx is copied first, so callers holding the previous
// context observe no change.
// WithRequestMetadataValue 返回把 key 设为 value 的 ctx 副本。
// ctx 中已有的元数据会先复制，持有旧 context 的调用方看不到变化.
func WithRequestMetadataValue(ctx context.Context, key, value string) context.Context {
	cur, _ := RequestMetadataKey.FromContext(ctx)
	next := cloneRequestMetadata(cur)
	if next == nil {
		next = make(RequestMetadata, 1)
	}
	next[key] = value
	return RequestMetadataKey.NewContext(ctx, next)
}

// WithRequestMetadata returns a copy of ctx with every entry of md merged into
// the existing request metadata, overwriting keys that already exist. ctx is
// returned unchanged when md is empty. The provided map is copied before it is
// stored.
// WithRequestMetadata 返回把 md 的每一项合并进既有元数据的 ctx 副本，已存在的
// key 会被覆盖；md 为空时原样返回 ctx。传入的 map 在存入前会先复制.
func WithRequestMetadata(ctx context.Context, md RequestMetadata) context.Context {
	if len(md) == 0 {
		return ctx
	}
	cur, _ := RequestMetadataKey.FromContext(ctx)
	next := cloneRequestMetadata(cur)
	if next == nil {
		next = make(RequestMetadata, len(md))
	}
	maps.Copy(next, md)
	return RequestMetadataKey.NewContext(ctx, next)
}

// WithoutRequestMetadata returns a copy of ctx with the given keys removed. ctx
// is returned unchanged when it carries no request metadata or no keys are
// given.
// WithoutRequestMetadata 返回移除给定 key 后的 ctx 副本；ctx 没有元数据或未给
// key 时原样返回.
func WithoutRequestMetadata(ctx context.Context, keys ...string) context.Context {
	if len(keys) == 0 {
		return ctx
	}
	cur, ok := RequestMetadataKey.FromContext(ctx)
	if !ok || len(cur) == 0 {
		return ctx
	}
	next := cloneRequestMetadata(cur)
	for _, k := range keys {
		delete(next, k)
	}
	return RequestMetadataKey.NewContext(ctx, next)
}

// cloneRequestMetadata returns a deep copy of md, or nil when md is nil.
// cloneRequestMetadata 返回 md 的深拷贝，md 为 nil 时返回 nil.
func cloneRequestMetadata(md RequestMetadata) RequestMetadata {
	if md == nil {
		return nil
	}
	out := make(RequestMetadata, len(md))
	maps.Copy(out, md)
	return out
}

// WithTraceID returns a new context with the given trace ID set in its
// [RequestMetadata]. It is a convenience wrapper around [WithRequestMetadataValue].
// WithTraceID 返回在 [RequestMetadata] 中设置 trace ID 的新 context，是
// [WithRequestMetadataValue] 的便捷封装.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return WithRequestMetadataValue(ctx, HeaderTraceID, traceID)
}

// GetTraceID returns the trace ID from the context's [RequestMetadata], or an
// empty string if none is set.
// GetTraceID 返回 context [RequestMetadata] 中的 trace ID，未设置时返回空串.
func GetTraceID(ctx context.Context) string {
	return GetRequestMetadata(ctx, HeaderTraceID)
}

// WithUserID returns a new context with the given user ID set in its
// [RequestMetadata]. It is a convenience wrapper around [WithRequestMetadataValue].
// WithUserID 返回在 [RequestMetadata] 中设置 user ID 的新 context，是
// [WithRequestMetadataValue] 的便捷封装.
func WithUserID(ctx context.Context, userID string) context.Context {
	return WithRequestMetadataValue(ctx, HeaderUserID, userID)
}

// GetUserID returns the user ID from the context's [RequestMetadata], or an
// empty string if none is set.
// GetUserID 返回 context [RequestMetadata] 中的 user ID，未设置时返回空串.
func GetUserID(ctx context.Context) string {
	return GetRequestMetadata(ctx, HeaderUserID)
}

// WithColorTag returns a new context with the given color tag set in its
// [RequestMetadata]. It is a convenience wrapper around [WithRequestMetadataValue].
// WithColorTag 返回在 [RequestMetadata] 中设置 color tag 的新 context，是
// [WithRequestMetadataValue] 的便捷封装.
func WithColorTag(ctx context.Context, tag string) context.Context {
	return WithRequestMetadataValue(ctx, HeaderColorTag, tag)
}

// GetColorTag returns the color tag from the context's [RequestMetadata], or an
// empty string if none is set.
// GetColorTag 返回 context [RequestMetadata] 中的 color tag，未设置时返回空串.
func GetColorTag(ctx context.Context) string {
	return GetRequestMetadata(ctx, HeaderColorTag)
}
