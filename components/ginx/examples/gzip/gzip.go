// Package gzip demonstrates response compression for clients that ask for it
// (Accept-Encoding: gzip); requests without it pass through untouched.
package gzip

import (
	"strings"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/gzip"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/report", func(c *gin.Context) {
		c.String(200, strings.Repeat("gala ", 2048)) // a payload worth compressing
	})
	return r
}
