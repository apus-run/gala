// Package server demonstrates a realistic ginx middleware chain: identity,
// logging, recovery and header hygiene engine-wide, then per-group protection
// (timeout, active limit, circuit breaker), with liveness kept outside the
// protected group. The Use order is the point of this example; each call says
// why it sits where it does. Swap the business handlers for your own.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/accesslog"
	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/locallimit"
	"github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/apus-run/gala/components/ginx/middlewares/cors"
	"github.com/apus-run/gala/components/ginx/middlewares/cost"
	"github.com/apus-run/gala/components/ginx/middlewares/gzip"
	"github.com/apus-run/gala/components/ginx/middlewares/helmet"
	"github.com/apus-run/gala/components/ginx/middlewares/recovery"
	"github.com/apus-run/gala/components/ginx/middlewares/requstid"
	"github.com/apus-run/gala/components/ginx/middlewares/timeout"
	"github.com/gin-gonic/gin"
)

// Server bundles the engine with the handles an operator needs: the breaker
// builder exposes its circuit for ForceOpen/ForceClose and Metrics.
type Server struct {
	Engine  *gin.Engine
	Breaker *circuitbreaker.Builder
}

// New assembles the chain. logAccess receives one entry per finished request;
// write it to your logger, MQ or both.
func New(logAccess func(context.Context, *accesslog.AccessLog)) *Server {
	breaker := circuitbreaker.NewBuilder().
		SetFailureThreshold(3).
		SetTimeout(10 * time.Second)

	r := gin.New()

	// Request ID first: everything downstream, the access log included, can
	// carry it; the response header is set for the client.
	r.Use(requstid.RequestID())

	// Access log before recovery: a recovered panic still produces its log line.
	// shortcut: StatusCode is only captured when AllowRespBody wraps the writer,
	// enable it if you need the status in the log; add AllowReqBody for bodies.
	r.Use(accesslog.NewBuilder(logAccess).MaxLength(1 << 10).AllowRespBody().Build())

	// Outer recovery also covers failures in header and compression middleware.
	r.Use(recovery.NewBuilder().Build())

	// Header hygiene applies engine-wide; CORS answers preflights before any
	// protected group can refuse them.
	r.Use(cors.NewCORS(
		cors.WithAllowOrigins([]string{"https://example.com"}),
		cors.WithAllowMethods([]string{http.MethodGet, http.MethodPost}),
	).Build())
	r.Use(helmet.NewBuilder().Build())
	r.Use(gzip.NewBuilder().Build())

	// Recover handler and timeout panics before gzip closes its response stream.
	r.Use(recovery.NewBuilder().Build())

	// Per-request latency of the whole chain, logged after it unwinds.
	r.Use(cost.NewBuilder().Build())

	api := r.Group("/api/v1")
	{
		// Timeout bounds the handler's context. shortcut: deadline expiry panics
		// today, so the global recovery answers 500, not 504; upgrade timeout
		// if you need the status distinction.
		api.Use(timeout.NewBuilder().SetTimeout(2 * time.Second).Build())

		// Local active limit caps in-flight work per process, before the
		// circuit counts refusals as traffic-independent state.
		api.Use(locallimit.NewLocalActiveLimit(100).Build())

		// Circuit breaker refuses the whole group while its dependency is down.
		api.Use(breaker.Build())

		api.GET("/orders/:id", getOrder)
	}

	// Liveness and breaker state stay outside the protected group: an open
	// circuit must not hide them from the load balancer.
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/debug/breaker", breaker.Breaker().HealthHandler())

	return &Server{Engine: r, Breaker: breaker}
}

// getOrder simulates one dependency behind the group: order "0" is always
// unreachable upstream, which the default failure detector counts (>= 500).
func getOrder(c *gin.Context) {
	id := c.Param("id")
	if id == "0" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "paid"})
}
