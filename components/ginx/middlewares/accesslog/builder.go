// Package accesslog provides a gin access-log middleware with a pluggable
// log sink | accesslog 提供日志出口可插拔的 gin 访问日志中间件.
//
// # Notes | 说明
//
// NewBuilder takes the sink that receives the assembled [AccessLog];
// request/response body logging is off by default and enabled per Builder.
// Bodies and URLs are truncated to MaxLength (1 KiB default); /ping, /pong
// and /health are skipped until IgnoreRoutes adds more. Enabling the request
// body reads it fully and rewinds it for downstream handlers.
// NewBuilder 接收汇聚 [AccessLog] 的输出函数；请求/响应体日志默认关闭，按
// Builder 显式开启。响应体与 URL 截断到 MaxLength（默认 1 KiB）；/ping、/pong、
// /health 默认跳过，IgnoreRoutes 可追加。开启请求体日志会整体读取并回填
// Body，供下游继续读取。
//
// # Example | 示例
//
//	b := accesslog.NewBuilder(func(ctx context.Context, al *accesslog.AccessLog) {
//		slog.Info("access", "url", al.URL, "duration", al.Duration)
//	}).AllowReqBody().Build()
//	r.Use(b)
package accesslog

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/atomic"
)

// Builder records HTTP request/response details | Builder 记录 HTTP 请求/响应细节.
type Builder struct {
	allowReqBody  *atomic.Bool
	allowRespBody *atomic.Bool

	// Max length of http response body and request URL | http 响应体与请求 URL 的最大长度.
	maxLength *atomic.Int64

	// Routes whose logs are skipped | 忽略指定路由的日志打印.
	ignoreRoutes map[string]struct{}

	logFunc func(ctx context.Context, al *AccessLog)
}

// NewBuilder creates a Builder using fn as the log sink
// | NewBuilder 创建 Builder，fn 为日志输出函数。
func NewBuilder(fn func(ctx context.Context, al *AccessLog)) *Builder {
	return &Builder{
		// Not logged by default | 默认不打印。
		allowReqBody:  atomic.NewBool(false),
		allowRespBody: atomic.NewBool(false),

		maxLength: atomic.NewInt64(1024), // 1 MiB

		ignoreRoutes: map[string]struct{}{
			"/ping":   {},
			"/pong":   {},
			"/health": {},
		},

		logFunc: fn,
	}
}

// AllowReqBody enables logging the request body | AllowReqBody 开启请求体日志。
func (b *Builder) AllowReqBody() *Builder {
	b.allowReqBody.Store(true)
	return b
}

// AllowRespBody enables logging the response body | AllowRespBody 开启响应体日志。
func (b *Builder) AllowRespBody() *Builder {
	b.allowRespBody.Store(true)
	return b
}

// MaxLength caps the logged body and URL length | MaxLength 限制日志中响应体与 URL 的长度。
func (b *Builder) MaxLength(maxLength int64) *Builder {
	b.maxLength.Store(maxLength)
	return b
}

// IgnoreRoutes adds routes whose logs are skipped | IgnoreRoutes 追加忽略日志的路由。
func (b *Builder) IgnoreRoutes(routes ...string) *Builder {
	for _, route := range routes {
		b.ignoreRoutes[route] = struct{}{}
	}

	return b
}

// Build returns the gin access-log middleware | Build 返回 gin 访问日志中间件。
func (b *Builder) Build() gin.HandlerFunc {
	pid := strconv.Itoa(os.Getpid())
	return func(c *gin.Context) {
		start := time.Now()
		maxLength := b.maxLength.Load()
		allowReqBody := b.allowReqBody.Load()
		allowRespBody := b.allowRespBody.Load()

		// ignore printing of the specified route
		// 忽略指定路由的日志打印
		if _, ok := b.ignoreRoutes[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		host := c.Request.Host
		// A Host without an explicit port (example.com) or an IPv6 literal must
		// not panic; net.SplitHostPort reports the missing port as an error.
		// 没有显式端口的 Host（如 example.com）或 IPv6 字面量不能 panic；
		// net.SplitHostPort 会把缺失端口报告为错误。
		ip, port := host, ""
		if h, p, err := net.SplitHostPort(host); err == nil {
			ip, port = h, p
		}

		// URLs can be very long, so protect against that
		// URL 有可能会很长，保护起来
		url := c.Request.URL
		urlStr := url.String()
		urlLen := int64(len(urlStr))
		if urlLen >= maxLength {
			urlStr = urlStr[:maxLength]
		}
		accessLog := &AccessLog{
			PID:      pid,
			Referer:  c.Request.Header.Get("Referer"),
			Protocol: url.Scheme,
			Port:     port,
			IP:       ip,
			IPs:      c.Request.Header.Get("X-Forwarded-For"),
			Host:     host,
			URL:      urlStr,
			UA:       c.Request.Header.Get("User-Agent"),

			Method: c.Request.Method,
			Path:   url.Path,
		}

		if allowReqBody && c.Request.Body != nil {
			// The error can be ignored; it does not affect the program.
			// GetRawData is implemented as io.ReadAll(c.Request.Body).
			// 可以直接忽略 error，不影响程序运行；
			// GetRawData 实现了 io.ReadAll(c.Request.Body)。
			body, _ := c.GetRawData()
			// Request.Body is a stream that can only be read once, so put it
			// back afterwards or later steps will read nothing.
			// Request.Body 是一个 Stream（流）对象，只能读取一次；
			// 读完之后要放回去，不然后续步骤是读不到的。
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

			// Guard against an oversized body | 防止 body 内容过大，保护起来
			if int64(len(body)) >= maxLength {
				body = body[:maxLength]
			}
			// Mind the resource cost | 注意资源的消耗
			accessLog.ReqBody = string(body)
		}

		if allowRespBody {
			c.Writer = responseWriter{
				al:             accessLog,
				ResponseWriter: c.Writer,
				maxLength:      maxLength,
			}
		}

		defer func() {
			duration := time.Since(start)
			accessLog.Duration = duration.String()
			b.logFunc(c, accessLog)
		}()

		c.Next()
	}
}
