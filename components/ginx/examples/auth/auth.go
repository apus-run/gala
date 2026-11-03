// Package auth demonstrates authentication middleware with a public login
// route and a protected profile route.
// Package auth 演示认证中间件：一个公开的登录路由和一个受保护的
// profile 路由。
package auth

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/authn"
	"github.com/apus-run/gala/components/ginx/middlewares/auth"
)

const demoToken = "demo-token"

// demoAuthFunc is a small AuthFunc for the example. Applications should use
// SessionAuth.AuthFunc or another real credential verifier.
// demoAuthFunc 是示例用的小型 AuthFunc。实际应用应使用
// SessionAuth.AuthFunc 或其他真实的凭证校验器。
func demoAuthFunc(_ context.Context, r *http.Request) (*authn.SessionClaims, error) {
	raw, ok := authn.TokenFromHeader(r)
	if !ok {
		return nil, authn.ErrMissingToken
	}
	if raw != demoToken {
		return nil, authn.ErrTokenInvalid
	}
	claims := &authn.SessionClaims{}
	claims.Subject = "alice"
	return claims, nil
}

// New builds the demo engine with login and protected routes | New 构建含登录路由与受保护路由的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(auth.NewBuilder(demoAuthFunc).
		IgnorePaths("/login").
		Build())
	r.POST("/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"token": demoToken})
	})
	r.GET("/profile", func(c *gin.Context) {
		claims, ok := authn.FromContext[*authn.SessionClaims](c.Request.Context())
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "identity missing"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user": claims.Subject})
	})
	return r
}
