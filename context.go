package gala

import (
	"context"

	"github.com/apus-run/gala/pkg/ctxkey"
)

var ServiceContextKey = ctxkey.NewContextKey[Gala]()

// Standard header keys used to propagate request metadata across
// service boundaries. Callers may use these keys or define their own.
const (
	HeaderTraceID  = "x-app-trace-id"
	HeaderUserID   = "x-app-user-id"
	HeaderColorTag = "x-app-color-tag"
)

// RequestMetadata is a simple string-keyed map carried through the context chain.
// It is the vehicle for trace IDs, user IDs, color tags and other
// request-scoped attributes.
type RequestMetadata map[string]string

// RequestMetadataKey stores request-scoped metadata in a context.
var RequestMetadataKey = ctxkey.NewContextKey[RequestMetadata]()

// Clone returns a deep copy of md. It returns nil when md is nil so that an
// absent metadata value round-trips cleanly.
func (m RequestMetadata) Clone() RequestMetadata {
	return cloneRequestMetadata(m)
}

// GetRequestMetadata returns the value stored under key, or "" if ctx carries
// no request metadata or the key is absent. It never exposes the underlying map.
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
func WithRequestMetadata(ctx context.Context, md RequestMetadata) context.Context {
	if len(md) == 0 {
		return ctx
	}
	cur, _ := RequestMetadataKey.FromContext(ctx)
	next := cloneRequestMetadata(cur)
	if next == nil {
		next = make(RequestMetadata, len(md))
	}
	for k, v := range md {
		next[k] = v
	}
	return RequestMetadataKey.NewContext(ctx, next)
}

// WithoutRequestMetadata returns a copy of ctx with the given keys removed. ctx
// is returned unchanged when it carries no request metadata or no keys are
// given.
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
func cloneRequestMetadata(md RequestMetadata) RequestMetadata {
	if md == nil {
		return nil
	}
	out := make(RequestMetadata, len(md))
	for k, v := range md {
		out[k] = v
	}
	return out
}

// WithTraceID returns a new context with the given trace ID set in its
// [RequestMetadata]. It is a convenience wrapper around [WithRequestMetadataValue].
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return WithRequestMetadataValue(ctx, HeaderTraceID, traceID)
}

// GetTraceID returns the trace ID from the context's [RequestMetadata], or an
// empty string if none is set.
func GetTraceID(ctx context.Context) string {
	return GetRequestMetadata(ctx, HeaderTraceID)
}

// WithUserID returns a new context with the given user ID set in its
// [RequestMetadata]. It is a convenience wrapper around [WithRequestMetadataValue].
func WithUserID(ctx context.Context, userID string) context.Context {
	return WithRequestMetadataValue(ctx, HeaderUserID, userID)
}

// GetUserID returns the user ID from the context's [RequestMetadata], or an
// empty string if none is set.
func GetUserID(ctx context.Context) string {
	return GetRequestMetadata(ctx, HeaderUserID)
}

// WithColorTag returns a new context with the given color tag set in its
// [RequestMetadata]. It is a convenience wrapper around [WithRequestMetadataValue].
func WithColorTag(ctx context.Context, tag string) context.Context {
	return WithRequestMetadataValue(ctx, HeaderColorTag, tag)
}

// GetColorTag returns the color tag from the context's [RequestMetadata], or an
// empty string if none is set.
func GetColorTag(ctx context.Context) string {
	return GetRequestMetadata(ctx, HeaderColorTag)
}
