// Package auth demonstrates the auth middleware skeleton: whitelisted paths
// pass straight through. shortcut: JWT validation is commented out upstream,
// so today every request passes; wire real token checking before trusting it.
package auth

import (
	middleware "github.com/apus-run/gala/components/ginx/middlewares/auth"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().
		IgnorePaths("/login").
		Build())
	r.POST("/login", func(c *gin.Context) { c.JSON(200, gin.H{"token": "..."}) })
	r.GET("/profile", func(c *gin.Context) { c.JSON(200, gin.H{"user": "alice"}) })
	return r
}
