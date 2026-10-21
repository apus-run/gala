// Package metrics demonstrates Prometheus instrumentation: Metrics registers
// the /metrics scrape endpoint on the engine and records count, duration and
// sizes per request, with ignore options for paths that would skew them.
package metrics

import (
	"net/http"

	"github.com/apus-run/gala/components/ginx/middlewares/metrics"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(metrics.Metrics(r)) // registers GET /metrics on r itself
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}
