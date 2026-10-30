// Package auth demonstrates authentication middleware with a public login
// route and a protected profile route.
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
