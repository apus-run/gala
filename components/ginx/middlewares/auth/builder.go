// Package auth turns an authn.AuthFunc into a gin authentication middleware |
// auth 把 authn.AuthFunc 包装成 gin 认证中间件.
//
// # Notes | 说明
//
// On success the claims go into the request context via authn.NewContext;
// downstream reads them with authn.FromContext using the same type T given
// to NewBuilder. Failures map through authn.StatusOf and may add a
// WWW-Authenticate header. IgnorePaths matches exactly (the Gin route
// template when available), IgnorePrefix matches by path segment. When no
// auth function is configured every non-exempt request is answered 503.
// 成功时 claims 经 authn.NewContext 写入请求 context；下游用 authn.FromContext
// 以 NewBuilder 的同一类型 T 读取。失败经 authn.StatusOf 映射状态码，可能附带
// WWW-Authenticate 头。IgnorePaths 精确匹配（优先用 Gin 路由模板），
// IgnorePrefix 按路径段匹配。未配置认证函数时，所有未豁免请求一律返回 503。
//
// # Example | 示例
//
//	// authFn is authn.AuthFunc[MyClaims]; MyClaims is inferred | authFn 为 authn.AuthFunc[MyClaims]，T 由其推断
//	mw := auth.NewBuilder(authFn).
//		IgnorePaths("/ping").
//		IgnorePrefix("/public").
//		Build()
//	r.Use(mw)
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/authn"
	"github.com/apus-run/gala/components/ginx"
)

// Builder builds the Gin authentication middleware. Configure it before Build;
// setters and Build are not safe to call concurrently.
// Builder 构建 Gin 认证中间件。需在 Build 前完成配置；
// setter 与 Build 不能并发调用。
type Builder struct {
	auth        func(context.Context, *http.Request) (any, error)
	exactPaths  []string
	prefixPaths []string
}

// NewBuilder creates a middleware builder from an authentication function.
// The function can return any identity type; use authn.FromContext with the
// same type downstream.
// NewBuilder 基于认证函数创建中间件构建器。该函数可返回任意身份类型；
// 下游请用同一类型调用 authn.FromContext。
func NewBuilder[T any](authFn authn.AuthFunc[T]) *Builder {
	b := &Builder{}
	if authFn != nil {
		b.auth = func(ctx context.Context, r *http.Request) (any, error) {
			return authFn(ctx, r)
		}
	}
	return b
}

// IgnorePaths exempts paths from authentication using an exact match.
// IgnorePaths 以精确匹配豁免路径的认证。
func (b *Builder) IgnorePaths(paths ...string) *Builder {
	b.exactPaths = append(b.exactPaths, paths...)
	return b
}

// IgnorePrefix exempts paths from authentication using path-segment prefix
// matching. IgnorePrefix("/public") matches "/public" and "/public/a", but not
// "/publicity".
// IgnorePrefix 以路径段前缀匹配豁免路径的认证。IgnorePrefix("/public")
// 匹配 "/public" 与 "/public/a"，但不匹配 "/publicity"。
func (b *Builder) IgnorePrefix(prefixes ...string) *Builder {
	b.prefixPaths = append(b.prefixPaths, prefixes...)
	return b
}

// Build returns the Gin middleware handler.
// Build 返回 Gin 中间件处理函数。
func (b *Builder) Build() gin.HandlerFunc {
	exactPaths := append([]string(nil), b.exactPaths...)
	prefixPaths := append([]string(nil), b.prefixPaths...)
	authFn := b.auth

	return ginx.Handle(func(ctx *ginx.Context) {
		path := ctx.Request.URL.Path
		fullPath := ctx.FullPath()
		if skipped(path, fullPath, exactPaths, prefixPaths) {
			ctx.Next()
			return
		}
		if authFn == nil {
			abortUnauthenticated(ctx, http.StatusServiceUnavailable)
			return
		}
		claims, err := authFn(ctx.Request.Context(), ctx.Request)
		if err != nil {
			code, challenge := authn.StatusOf(err)
			if challenge != "" {
				ctx.Header("WWW-Authenticate", challenge)
			}
			abortUnauthenticated(ctx, code)
			return
		}
		ctx.Request = ctx.Request.WithContext(authn.NewContext(ctx.Request.Context(), claims))
		ctx.Next()
	})
}

func skipped(path, fullPath string, exactPaths, prefixPaths []string) bool {
	// Prefer Gin route template for exact matching when available so URL path
	// normalization cannot make a whitelist entry match an unintended route.
	// 优先用 Gin 路由模板做精确匹配，避免 URL 规范化使白名单条目误中其他路由。
	exactPath := path
	if fullPath != "" {
		exactPath = fullPath
	}
	for _, candidate := range exactPaths {
		if candidate != "" && exactPath == candidate {
			return true
		}
	}
	for _, prefix := range prefixPaths {
		if prefix == "" {
			continue
		}
		if hasPathPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// hasPathPrefix requires a path-segment boundary after prefix so /public does
// not accidentally exempt /publicity.
// hasPathPrefix 要求 prefix 之后是路径段边界，避免 /public 误豁免 /publicity。
func hasPathPrefix(path, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(path, "/")
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return false
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/'
}

func abortUnauthenticated(ctx *ginx.Context, code int) {
	ctx.AbortWithStatusJSON(code, ginx.Result{
		Code: code,
		Msg:  "unauthenticated",
		Data: gin.H{},
	})
}
