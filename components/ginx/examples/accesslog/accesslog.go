// Package accesslog demonstrates structured per-request logging: one entry per
// finished request with method, path, status, duration and - when allowed -
// request and response bodies, truncated to MaxLength.
package accesslog

import (
	"context"
	"log"

	"github.com/apus-run/gala/components/ginx/middlewares/accesslog"
	"github.com/gin-gonic/gin"
)

func New(sink func(context.Context, *accesslog.AccessLog)) *gin.Engine {
	r := gin.New()
	r.Use(accesslog.NewBuilder(sink).
		AllowReqBody().
		AllowRespBody().
		MaxLength(1024).
		IgnoreRoutes("/healthz").
		Build())
	r.GET("/echo", func(c *gin.Context) { c.JSON(200, gin.H{"msg": "hi"}) })
	r.GET("/healthz", func(c *gin.Context) { c.Status(200) })
	return r
}

// Stdout is the simplest production sink: JSON to the default logger.
func Stdout() func(context.Context, *accesslog.AccessLog) {
	return func(_ context.Context, al *accesslog.AccessLog) { log.Printf("%+v", *al) }
}
