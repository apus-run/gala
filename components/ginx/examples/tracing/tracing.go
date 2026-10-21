// Package tracing demonstrates OpenTelemetry span creation per request: with
// no provider configured it uses the global (noop) one, so this wiring is safe
// in tests and activates itself once the process registers a real tracer.
package tracing

import (
	"net/http"

	"github.com/apus-run/gala/components/ginx/middlewares/tracing"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(tracing.Tracing("order-service"))
	r.GET("/orders", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"orders": []int{1}}) })
	return r
}
