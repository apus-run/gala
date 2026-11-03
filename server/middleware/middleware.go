// Package middleware provides a protocol-independent handler and middleware chain.
// middleware 提供与协议无关的 Handler 与中间件链.
package middleware

import (
	"context"

	"github.com/apus-run/gala/pkg/chain"
)

// Handler processes a request. The request and response types belong to the
// protocol adapter; reusable middleware generally operates on ctx and err.
// Handler 处理一次请求。请求与响应类型属于协议适配器，可复用的中间件一般只
// 操作 ctx 与 err.
type Handler func(ctx context.Context, req any) (any, error)

// Middleware decorates a handler. Call next synchronously, at most once.
// Returning without calling next short-circuits the request. Middleware must be
// safe for concurrent requests and must not retain request-scoped objects.
// Middleware 装饰一个 Handler。同步调用 next 且最多一次；不调用 next 直接返回
// 即短路请求。Middleware 必须并发安全，且不得留存请求级对象.
type Middleware func(Handler) Handler

// Chain assembles middlewares so that the first argument is the outermost
// (executed first) and the last is the innermost (closest to the handler).
//
//	Chain(m1, m2, m3)(h) == m1(m2(m3(h)))
//
// Chain 组装中间件：第一个参数最外层（先执行），最后一个最内层（最靠近 Handler）.
func Chain(outer Middleware, others ...Middleware) Middleware {
	return chain.Chain[Handler, Middleware](outer, others...)
}
