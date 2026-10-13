// Package middleware provides a protocol-independent handler and middleware chain.
package middleware

import (
	"context"

	"github.com/apus-run/gala/pkg/chain"
)

// Handler processes a request. The request and response types belong to the
// protocol adapter; reusable middleware generally operates on ctx and err.
type Handler func(ctx context.Context, req any) (any, error)

// Middleware decorates a handler. Call next synchronously, at most once.
// Returning without calling next short-circuits the request. Middleware must be
// safe for concurrent requests and must not retain request-scoped objects.
type Middleware func(Handler) Handler

// Chain assembles middlewares so that the first argument is the outermost
// (executed first) and the last is the innermost (closest to the handler).
//
//	Chain(m1, m2, m3)(h) == m1(m2(m3(h)))
func Chain(outer Middleware, others ...Middleware) Middleware {
	return chain.Chain[Handler, Middleware](outer, others...)
}
