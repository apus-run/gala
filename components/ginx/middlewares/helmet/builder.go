// Copyright (c) 2019-present Fenny and Contributors.
// Adapted from gofiber/fiber/v3/middleware/helmet under the MIT License; see LICENSE.

// Package helmet adds configurable security response headers to Gin requests.
// helmet 为 Gin 请求添加可配置的安全响应头。
package helmet

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Builder creates Helmet handlers without retaining configuration or request state.
// Each Build captures its own configuration snapshot.
// Builder 创建 Helmet handler，不保留配置或请求状态；每次 Build 都捕获自己的配置快照。
type Builder struct{}

// NewBuilder returns a stateless Helmet builder.
// NewBuilder 返回无状态的 Helmet 构建器。
func NewBuilder() *Builder {
	return &Builder{}
}

// Build creates a Gin middleware with the upstream Helmet configuration semantics.
// Only the first optional Config is used, as in Fiber Helmet.
// Build 按上游 Helmet 语义创建 Gin 中间件。与 Fiber Helmet 一样只使用第一个可选 Config。
func (b *Builder) Build(config ...Config) gin.HandlerFunc {
	// Init config | 初始化配置
	cfg := configDefault(config...)

	// The Strict-Transport-Security value only depends on config, so build it
	// once instead of formatting it on every secure request.
	// Strict-Transport-Security 的值只取决于配置，因此只构建一次，
	// 不在每个安全请求上重复格式化。
	hstsHeaderValue := ""
	if cfg.HSTSMaxAge > 0 {
		var header strings.Builder
		header.WriteString("max-age=")                                   //nolint:errcheck // strings.Builder cannot fail
		header.WriteString(strconv.FormatInt(int64(cfg.HSTSMaxAge), 10)) //nolint:errcheck // strings.Builder cannot fail
		if !cfg.HSTSExcludeSubdomains {
			header.WriteString("; includeSubDomains") //nolint:errcheck // strings.Builder cannot fail
		}
		if cfg.HSTSPreloadEnabled {
			header.WriteString("; preload") //nolint:errcheck // strings.Builder cannot fail
		}
		hstsHeaderValue = header.String()
	}

	isSecure := cfg.IsSecure
	if isSecure == nil {
		isSecure = func(c *gin.Context) bool { return c.Request.TLS != nil }
	}

	// Return middleware handler | 返回中间件 handler
	return func(c *gin.Context) {
		// Next request to skip middleware | 命中 Next 的请求跳过中间件
		if cfg.Next != nil && cfg.Next(c) {
			c.Next()
			return
		}

		// Set headers
		if cfg.XSSProtection != "" {
			c.Header("X-XSS-Protection", cfg.XSSProtection)
		}

		if cfg.ContentTypeNosniff != "" {
			c.Header("X-Content-Type-Options", cfg.ContentTypeNosniff)
		}

		if cfg.XFrameOptions != "" {
			c.Header("X-Frame-Options", cfg.XFrameOptions)
		}

		if cfg.CrossOriginEmbedderPolicy != "" {
			c.Header("Cross-Origin-Embedder-Policy", cfg.CrossOriginEmbedderPolicy)
		}

		if cfg.CrossOriginOpenerPolicy != "" {
			c.Header("Cross-Origin-Opener-Policy", cfg.CrossOriginOpenerPolicy)
		}

		if cfg.CrossOriginResourcePolicy != "" {
			c.Header("Cross-Origin-Resource-Policy", cfg.CrossOriginResourcePolicy)
		}

		if cfg.OriginAgentCluster != "" {
			c.Header("Origin-Agent-Cluster", cfg.OriginAgentCluster)
		}

		if cfg.ReferrerPolicy != "" {
			c.Header("Referrer-Policy", cfg.ReferrerPolicy)
		}

		if cfg.XDNSPrefetchControl != "" {
			c.Header("X-DNS-Prefetch-Control", cfg.XDNSPrefetchControl)
		}

		if cfg.XDownloadOptions != "" {
			c.Header("X-Download-Options", cfg.XDownloadOptions)
		}

		if cfg.XPermittedCrossDomain != "" {
			c.Header("X-Permitted-Cross-Domain-Policies", cfg.XPermittedCrossDomain)
		}

		// Handle HSTS headers | 处理 HSTS 响应头
		if hstsHeaderValue != "" && isSecure(c) {
			c.Header("Strict-Transport-Security", hstsHeaderValue)
		}

		// Handle Content-Security-Policy headers | 处理 Content-Security-Policy 响应头
		if cfg.ContentSecurityPolicy != "" {
			if cfg.CSPReportOnly {
				c.Header("Content-Security-Policy-Report-Only", cfg.ContentSecurityPolicy)
			} else {
				c.Header("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}
		}

		// Handle Permissions-Policy headers | 处理 Permissions-Policy 响应头
		if cfg.PermissionPolicy != "" {
			c.Header("Permissions-Policy", cfg.PermissionPolicy)
		}

		c.Next()
	}
}
