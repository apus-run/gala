// Package server demonstrates a realistic ginx middleware chain: identity,
// logging, recovery and header hygiene engine-wide, then per-group protection
// (timeout, active limit, circuit breaker), with liveness kept outside the
// protected group. The Use order is the point of this example; each call says
// why it sits where it does. Swap the business handlers for your own.
// Package server 演示一条贴近真实的 ginx 中间件链：身份、日志、recover 和
// 响应头治理覆盖整个引擎，之后是组级防护（超时、活跃请求上限、熔断器），
// 存活检查放在受保护组之外。本示例的重点在于 Use 的顺序；每次调用都说明
// 自己为何放在这个位置。业务 handler 请替换为你自己的实现。
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
// Server 将引擎与运维所需的句柄打包在一起：熔断器 builder 暴露其熔断器，
// 可执行 ForceOpen/ForceClose 和 Metrics。
type Server struct {
	Engine  *gin.Engine
	Breaker *circuitbreaker.Builder
}

// New assembles the chain. logAccess receives one entry per finished request;
// write it to your logger, MQ or both.
// New 组装整条链。logAccess 每收到一个完成的请求就得到一条记录；
// 可写入你的日志器、MQ 或两者。
func New(logAccess func(context.Context, *accesslog.AccessLog)) *Server {
	breaker := circuitbreaker.NewBuilder().
		SetFailureThreshold(3).
		SetTimeout(10 * time.Second)

	r := gin.New()

	// Request ID first: everything downstream, the access log included, can
	// carry it; the response header is set for the client.
	// 请求 ID 放在最前：包括访问日志在内的所有下游中间件都能携带它，
	// 响应头中也会为客户端设置该 ID。
	r.Use(requstid.RequestID())

	// Access log before recovery: a recovered panic still produces its log line.
	// shortcut: StatusCode is only captured when AllowRespBody wraps the writer,
	// enable it if you need the status in the log; add AllowReqBody for bodies.
	// 访问日志放在 recovery 之前：被恢复的 panic 仍会产出日志行。
	// shortcut: 只有 AllowRespBody 包装了 writer 时才会记录 StatusCode，
	// 若需要日志里的状态码请启用它；响应体则另加 AllowReqBody。
	r.Use(accesslog.NewBuilder(logAccess).MaxLength(1 << 10).AllowRespBody().Build())

	// Outer recovery also covers failures in header and compression middleware.
	// 外层 recovery 同时覆盖头部与压缩中间件中的失败。
	r.Use(recovery.NewBuilder().Build())

	// Header hygiene applies engine-wide; CORS answers preflights before any
	// protected group can refuse them.
	// 响应头治理覆盖整个引擎；CORS 在任何受保护组拒绝之前先应答预检请求。
	r.Use(cors.NewCORS(
		cors.WithAllowOrigins([]string{"https://example.com"}),
		cors.WithAllowMethods([]string{http.MethodGet, http.MethodPost}),
	).Build())
	r.Use(helmet.NewBuilder().Build())
	r.Use(gzip.NewBuilder().Build())

	// Recover handler and timeout panics before gzip closes its response stream.
	// 在 gzip 关闭其响应流之前，恢复 handler 与超时引发的 panic。
	r.Use(recovery.NewBuilder().Build())

	// Per-request latency of the whole chain, logged after it unwinds.
	// 整条链的单请求耗时，在链路回溯完成后记录。
	r.Use(cost.NewBuilder().Build())

	api := r.Group("/api/v1")
	{
		// Timeout bounds the handler's context. shortcut: deadline expiry panics
		// today, so the global recovery answers 500, not 504; upgrade timeout
		// if you need the status distinction.
		// Timeout 限定 handler 的上下文。shortcut: 目前到达截止时间会 panic，
		// 因此由全局 recovery 回 500 而非 504；若需要区分状态码，请升级
		// timeout。
		api.Use(timeout.NewBuilder().SetTimeout(2 * time.Second).Build())

		// Local active limit caps in-flight work per process, before the
		// circuit counts refusals as traffic-independent state.
		// 本地活跃上限按进程限制在途工作量，且置于熔断器之前，使熔断的
		// 拒绝与流量无关。
		api.Use(locallimit.NewLocalActiveLimit(100).Build())

		// Circuit breaker refuses the whole group while its dependency is down.
		// 依赖宕机期间，熔断器拒绝整个组的请求。
		api.Use(breaker.Build())

		api.GET("/orders/:id", getOrder)
	}

	// Liveness and breaker state stay outside the protected group: an open
	// circuit must not hide them from the load balancer.
	// 存活检查与熔断器状态放在受保护组之外：熔断打开时不能让负载均衡器
	// 看不到它们。
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/debug/breaker", breaker.Breaker().HealthHandler())

	return &Server{Engine: r, Breaker: breaker}
}

// getOrder simulates one dependency behind the group: order "0" is always
// unreachable upstream, which the default failure detector counts (>= 500).
// getOrder 模拟该组背后的一个依赖：order "0" 的上游永远不可达，
// 会被默认的失败判定器计数（>= 500）。
func getOrder(c *gin.Context) {
	id := c.Param("id")
	if id == "0" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "paid"})
}
