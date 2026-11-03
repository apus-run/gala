// Copyright (c) 2019-present Fenny and Contributors.
// Adapted from gofiber/fiber/v3/middleware/helmet under the MIT License; see LICENSE.

package helmet

import (
	"github.com/gin-gonic/gin"
)

// Config defines the security response headers. Build snapshots the first Config.
// Callbacks must be concurrency-safe and must not retain gin.Context.
// Config 定义安全响应头。Build 快照第一个 Config；回调必须并发安全，
// 且不得保留 gin.Context。
type Config struct {
	// Next defines a function to skip middleware.
	// Optional. Default: nil
	// Next 定义跳过中间件的判定函数。可选，默认 nil。
	Next func(*gin.Context) bool

	// IsSecure adapts Fiber's framework-level Secure check to Gin. If nil,
	// only an actual TLS connection is considered secure. A custom check must
	// verify the immediate proxy before accepting forwarded scheme headers.
	// Optional. Default: nil (Request.TLS != nil).
	// IsSecure 把 Fiber 框架级 Secure 检查适配到 Gin。为 nil 时只有真实 TLS
	// 连接视为安全。自定义检查在接受转发协议头之前必须先验证直连代理。
	// 可选，默认 nil（Request.TLS != nil）。
	IsSecure func(*gin.Context) bool

	// XSSProtection | X-XSS-Protection 头
	// Optional. Default value "0". | 可选，默认 "0"。
	XSSProtection string

	// ContentTypeNosniff | X-Content-Type-Options 头
	// Optional. Default value "nosniff". | 可选，默认 "nosniff"。
	ContentTypeNosniff string

	// XFrameOptions | X-Frame-Options 头
	// Optional. Default value "SAMEORIGIN".
	// Possible values: "SAMEORIGIN", "DENY", "ALLOW-FROM uri"
	// 可选，默认 "SAMEORIGIN"；可选值："SAMEORIGIN"、"DENY"、"ALLOW-FROM uri"。
	XFrameOptions string

	// ContentSecurityPolicy | Content-Security-Policy 头
	// Optional. Default value "". | 可选，默认空。
	ContentSecurityPolicy string

	// ReferrerPolicy | Referrer-Policy 头
	// Optional. Default value "no-referrer". | 可选，默认 "no-referrer"。
	ReferrerPolicy string

	// Permissions-Policy | Permissions-Policy 头
	// Optional. Default value "". | 可选，默认空。
	PermissionPolicy string

	// Cross-Origin-Embedder-Policy | COEP 头
	// Optional. Default value "require-corp". | 可选，默认 "require-corp"。
	CrossOriginEmbedderPolicy string

	// Cross-Origin-Opener-Policy | COOP 头
	// Optional. Default value "same-origin". | 可选，默认 "same-origin"。
	CrossOriginOpenerPolicy string

	// Cross-Origin-Resource-Policy | CORP 头
	// Optional. Default value "same-origin". | 可选，默认 "same-origin"。
	CrossOriginResourcePolicy string

	// Origin-Agent-Cluster | Origin-Agent-Cluster 头
	// Optional. Default value "?1". | 可选，默认 "?1"。
	OriginAgentCluster string

	// X-DNS-Prefetch-Control | X-DNS-Prefetch-Control 头
	// Optional. Default value "off". | 可选，默认 "off"。
	XDNSPrefetchControl string

	// X-Download-Options | X-Download-Options 头
	// Optional. Default value "noopen". | 可选，默认 "noopen"。
	XDownloadOptions string

	// X-Permitted-Cross-Domain-Policies | X-Permitted-Cross-Domain-Policies 头
	// Optional. Default value "none". | 可选，默认 "none"。
	XPermittedCrossDomain string

	// HSTSMaxAge | HSTS max-age
	// Optional. Default value 0. | 可选，默认 0。
	HSTSMaxAge int

	// HSTSExcludeSubdomains | 是否排除子域
	// Optional. Default value false. | 可选，默认 false。
	HSTSExcludeSubdomains bool

	// CSPReportOnly | CSP 仅上报模式
	// Optional. Default value false. | 可选，默认 false。
	CSPReportOnly bool

	// HSTSPreloadEnabled
	// Requires HSTSExcludeSubdomains to be false.
	// Optional. Default value false.
	// 启用 HSTS preload；要求 HSTSExcludeSubdomains 为 false。可选，默认 false。
	HSTSPreloadEnabled bool
}

// ConfigDefault is the upstream default config. Configure it before creating
// handlers; do not mutate this variable concurrently with Build.
// ConfigDefault 是上游默认配置。创建 handler 前请先配置；
// 不要与 Build 并发修改此变量。
var ConfigDefault = Config{
	XSSProtection:             "0",
	ContentTypeNosniff:        "nosniff",
	XFrameOptions:             "SAMEORIGIN",
	ReferrerPolicy:            "no-referrer",
	CrossOriginEmbedderPolicy: "require-corp",
	CrossOriginOpenerPolicy:   "same-origin",
	CrossOriginResourcePolicy: "same-origin",
	OriginAgentCluster:        "?1",
	XDNSPrefetchControl:       "off",
	XDownloadOptions:          "noopen",
	XPermittedCrossDomain:     "none",
}

// Helper function to set default values | 填充默认值的辅助函数
func configDefault(config ...Config) Config {
	// Return default config if nothing provided | 未提供配置时返回默认配置
	if len(config) < 1 {
		return ConfigDefault
	}

	// Override default config | 以提供的配置覆盖默认配置
	cfg := config[0]

	if cfg.HSTSMaxAge < 0 {
		panic("helmet: HSTSMaxAge must be greater than or equal to 0")
	}

	if cfg.HSTSPreloadEnabled && cfg.HSTSExcludeSubdomains {
		panic("helmet: HSTSPreloadEnabled requires HSTSExcludeSubdomains to be false")
	}

	// Set default values | 填充默认值
	if cfg.XSSProtection == "" {
		cfg.XSSProtection = ConfigDefault.XSSProtection
	}

	if cfg.ContentTypeNosniff == "" {
		cfg.ContentTypeNosniff = ConfigDefault.ContentTypeNosniff
	}

	if cfg.XFrameOptions == "" {
		cfg.XFrameOptions = ConfigDefault.XFrameOptions
	}

	if cfg.ReferrerPolicy == "" {
		cfg.ReferrerPolicy = ConfigDefault.ReferrerPolicy
	}

	if cfg.CrossOriginEmbedderPolicy == "" {
		cfg.CrossOriginEmbedderPolicy = ConfigDefault.CrossOriginEmbedderPolicy
	}

	if cfg.CrossOriginOpenerPolicy == "" {
		cfg.CrossOriginOpenerPolicy = ConfigDefault.CrossOriginOpenerPolicy
	}

	if cfg.CrossOriginResourcePolicy == "" {
		cfg.CrossOriginResourcePolicy = ConfigDefault.CrossOriginResourcePolicy
	}

	if cfg.OriginAgentCluster == "" {
		cfg.OriginAgentCluster = ConfigDefault.OriginAgentCluster
	}

	if cfg.XDNSPrefetchControl == "" {
		cfg.XDNSPrefetchControl = ConfigDefault.XDNSPrefetchControl
	}

	if cfg.XDownloadOptions == "" {
		cfg.XDownloadOptions = ConfigDefault.XDownloadOptions
	}

	if cfg.XPermittedCrossDomain == "" {
		cfg.XPermittedCrossDomain = ConfigDefault.XPermittedCrossDomain
	}

	return cfg
}
