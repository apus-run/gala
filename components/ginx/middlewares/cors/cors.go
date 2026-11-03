// Package cors provides a gin CORS middleware | cors 提供 gin 的 CORS 中间件.
//
// # Notes | 说明
//
// Defaults allow all origins (["*"]) with the common simple methods and
// headers; preflight OPTIONS requests end with 204 and never reach the
// business handler. Origin matching is exact value or "*" only — no pattern
// or regex support. Credentials together with "*" need explicit options;
// browsers reject that combination.
// 默认放行所有来源（["*"]）及常用简单方法与请求头；预检 OPTIONS 请求以 204
// 结束，不会到达业务 handler。来源匹配仅支持精确值或 "*"，无通配与正则。
// 凭据与 "*" 组合需显式配置——浏览器会拒绝该组合。
//
// # Example | 示例
//
//	r.Use(cors.NewCORS(cors.WithAllowOrigins([]string{"https://a.com"})).Build())
package cors

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	allowOrigin      = "Access-Control-Allow-Origin"
	allowMethods     = "Access-Control-Allow-Methods"
	allowHeaders     = "Access-Control-Allow-Headers"
	allowCredentials = "Access-Control-Allow-Credentials"
	exposeHeaders    = "Access-Control-Expose-Headers"
	requestMethod    = "Access-Control-Request-Method"
	requestHeaders   = "Access-Control-Request-Headers"
	maxAgeHeader     = "Access-Control-Max-Age"
	varyHeader       = "Vary"
	originHeader     = "Origin"
)

// defaultCORS default cors | defaultCORS 默认 CORS 配置。
var defaultCORS = CORS{
	allowCredentials: false,
	allowHeaders:     []string{"Content-Type", "Origin", "X-CSRF-Token", "Authorization", "AccessToken", "Token", "Range"},
	maxAge:           12 * time.Hour,
	allowOrigins:     []string{"*"},
	exposeHeaders:    []string{"Content-Length", "Access-Control-Allow-Origin", "Access-Control-Allow-Headers"},
	allowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete},
}

// CORS cors | CORS 跨域配置.
type CORS struct {
	// AllowedOrigins is a list of origins a cross-domain request can be executed from.
	// If the special "*" value is present in the list, all origins will be allowed.
	// Default value is ["*"]
	// 允许发起跨域请求的来源列表。列表含特殊值 "*" 时放行所有来源。默认 ["*"]。
	allowOrigins []string

	// AllowedMethods is a list of methods the client is allowed to use with
	// cross-domain requests. Default value is simple methods (GET and POST)
	// 允许客户端跨域使用的方法列表。默认为简单方法（GET 与 POST）。
	allowMethods []string

	// AllowedHeaders is list of non simple headers the client is allowed to use with
	// cross-domain requests.
	// If the special "*" value is present in the list, all headers will be allowed.
	// Default value is [] but "Origin" is always appended to the list.
	// 允许客户端跨域使用的非简单头列表。列表含 "*" 时放行所有头。
	// 默认为空，但 "Origin" 始终会被追加。
	allowHeaders []string

	// ExposedHeaders indicates which headers are safe to expose to the API of a CORS
	// API specification
	// 按 CORS 规范可安全暴露给 API 调用方的头。
	exposeHeaders []string

	// AllowCredentials indicates whether the request can include user credentials like
	// cookies, HTTP authentication or client side SSL certificates.
	// 请求是否可携带 cookie、HTTP 认证或客户端 SSL 证书等用户凭据。
	allowCredentials bool

	// MaxAge indicates how long (in seconds) the results of a preflight request
	// can be cached
	// 预检请求结果可缓存多久（秒）。
	maxAge time.Duration
}

// CORSOption configures CORS | CORSOption 配置 CORS。
type CORSOption interface {
	apply(*CORS)
}

// CORSOptionFunc adapts a plain function to CORSOption.
// CORSOptionFunc 把普通函数适配为 CORSOption。
type CORSOptionFunc func(*CORS)

func (f CORSOptionFunc) apply(o *CORS) {
	f(o)
}

// WithAllowOrigins set AllowOrigins | WithAllowOrigins 设置允许的来源。
func WithAllowOrigins(allowOrigins []string) CORSOption {
	return CORSOptionFunc(func(c *CORS) {
		c.allowOrigins = allowOrigins
	})
}

// WithAllowHeaders set AllowHeaders | WithAllowHeaders 设置允许的请求头。
func WithAllowHeaders(allowHeaders []string) CORSOption {
	return CORSOptionFunc(func(c *CORS) {
		c.allowHeaders = allowHeaders
	})
}

// WithAllowMethods set AllowMethods | WithAllowMethods 设置允许的请求方法。
func WithAllowMethods(allowMethods []string) CORSOption {
	return CORSOptionFunc(func(c *CORS) {
		c.allowMethods = allowMethods
	})
}

// WithExposeHeaders set ExposeHeaders | WithExposeHeaders 设置暴露的响应头。
func WithExposeHeaders(exposeHeaders []string) CORSOption {
	return CORSOptionFunc(func(c *CORS) {
		c.exposeHeaders = exposeHeaders
	})
}

// WithAllowCredentials set AllowCredentials | WithAllowCredentials 设置是否允许携带凭据。
func WithAllowCredentials(allowCredentials bool) CORSOption {
	return CORSOptionFunc(func(c *CORS) {
		c.allowCredentials = allowCredentials
	})
}

// WithMaxAge set MaxAge | WithMaxAge 设置预检结果缓存时长。
func WithMaxAge(maxAge time.Duration) CORSOption {
	return CORSOptionFunc(func(c *CORS) {
		c.maxAge = maxAge
	})
}

// NewCORS creates a CORS from the defaults plus opts | NewCORS 基于默认值加 opts 创建 CORS。
func NewCORS(opts ...CORSOption) *CORS {
	cors := defaultCORS
	for _, opt := range opts {
		opt.apply(&cors)
	}

	return &cors
}

// Build returns the CORS middleware | Build 返回 CORS 中间件。
func (m *CORS) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Add(varyHeader, originHeader)
		if c.Request.Method == http.MethodOptions {
			c.Writer.Header().Add(varyHeader, requestMethod)
			c.Writer.Header().Add(varyHeader, requestHeaders)
		}

		origin := c.Request.Header.Get(originHeader)
		if b, allowOrigins := isOriginAllowed(m.allowOrigins, origin); b {
			m.setCORSHeader(c, allowOrigins)
		}

		if c.Request.Method == http.MethodOptions {
			// Preflight ends here; the business handler must not run.
			// 预检请求到此为止，不能继续执行业务 handler
			c.AbortWithStatus(http.StatusNoContent)

			return
		}

		c.Next()
	}
}

// setCORSHeader set cors header | setCORSHeader 设置 CORS 响应头。
func (m *CORS) setCORSHeader(c *gin.Context, origins []string) {
	c.Writer.Header().Set(allowOrigin, strings.Join(origins, ","))
	c.Writer.Header().Set(allowMethods, strings.Join(m.allowMethods, ","))
	c.Writer.Header().Set(allowHeaders, strings.Join(m.allowHeaders, ","))
	c.Writer.Header().Set(exposeHeaders, strings.Join(m.exposeHeaders, ","))
	if m.allowCredentials {
		c.Writer.Header().Set(allowCredentials, "true")
	}
	if m.maxAge > time.Duration(0) {
		c.Writer.Header().Set(maxAgeHeader, strconv.FormatInt(int64(m.maxAge/time.Second), 10))
	}
}

// isOriginAllowed reports whether origin is allowed, with the origins to echo
// | isOriginAllowed 判断来源是否放行，并返回应回显的 origins。
func isOriginAllowed(allows []string, origin string) (bool, []string) {
	for _, o := range allows {
		if o == "*" {
			return true, []string{"*"}
		}

		if o == origin {
			return true, []string{origin}
		}
	}

	return false, []string{}
}
