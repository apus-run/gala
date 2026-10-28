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
type Builder struct {
	auth        func(context.Context, *http.Request) (any, error)
	exactPaths  []string
	prefixPaths []string
}

// NewBuilder creates a middleware builder from an authentication function.
// The function can return any identity type; use authn.FromContext with the
// same type downstream.
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
func (b *Builder) IgnorePaths(paths ...string) *Builder {
	b.exactPaths = append(b.exactPaths, paths...)
	return b
}

// IgnorePrefix exempts paths from authentication using path-segment prefix
// matching. IgnorePrefix("/public") matches "/public" and "/public/a", but not
// "/publicity".
func (b *Builder) IgnorePrefix(prefixes ...string) *Builder {
	b.prefixPaths = append(b.prefixPaths, prefixes...)
	return b
}

// Build returns the Gin middleware handler.
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
