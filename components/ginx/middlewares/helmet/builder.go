// Copyright (c) 2019-present Fenny and Contributors.
// Adapted from gofiber/fiber/v3/middleware/helmet under the MIT License; see LICENSE.

// Package helmet adds configurable security response headers to Gin requests.
package helmet

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Builder creates Helmet handlers without retaining configuration or request state.
// Each Build captures its own configuration snapshot.
type Builder struct{}

// NewBuilder returns a stateless Helmet builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Build creates a Gin middleware with the upstream Helmet configuration semantics.
// Only the first optional Config is used, as in Fiber Helmet.
func (b *Builder) Build(config ...Config) gin.HandlerFunc {
	// Init config
	cfg := configDefault(config...)

	// The Strict-Transport-Security value only depends on config, so build it
	// once instead of formatting it on every secure request.
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

	// Return middleware handler
	return func(c *gin.Context) {
		// Next request to skip middleware
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

		// Handle HSTS headers
		if hstsHeaderValue != "" && isSecure(c) {
			c.Header("Strict-Transport-Security", hstsHeaderValue)
		}

		// Handle Content-Security-Policy headers
		if cfg.ContentSecurityPolicy != "" {
			if cfg.CSPReportOnly {
				c.Header("Content-Security-Policy-Report-Only", cfg.ContentSecurityPolicy)
			} else {
				c.Header("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}
		}

		// Handle Permissions-Policy headers
		if cfg.PermissionPolicy != "" {
			c.Header("Permissions-Policy", cfg.PermissionPolicy)
		}

		c.Next()
	}
}
