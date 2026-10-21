// Package cors demonstrates browser access control for one trusted origin:
// preflights are answered before any business middleware runs, and only the
// allowed origin and methods are echoed back.
package cors

import (
	"net/http"

	"github.com/apus-run/gala/components/ginx/middlewares/cors"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(cors.NewCORS(
		cors.WithAllowOrigins([]string{"https://app.example.com"}),
		cors.WithAllowMethods([]string{http.MethodGet, http.MethodPost}),
		cors.WithAllowHeaders([]string{"Authorization", "Content-Type"}),
		cors.WithAllowCredentials(true),
		cors.WithMaxAge(12*3600),
	).Build())
	r.POST("/orders", func(c *gin.Context) { c.JSON(200, gin.H{"created": true}) })
	return r
}
