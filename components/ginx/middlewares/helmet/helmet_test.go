// Copyright (c) 2019-present Fenny and Contributors.
// Adapted from gofiber/fiber/v3/middleware/helmet under the MIT License; see LICENSE.

package helmet

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func Test_Default(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build())

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, "0", resp.Header().Get("X-XSS-Protection"))
	require.Equal(t, "nosniff", resp.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "SAMEORIGIN", resp.Header().Get("X-Frame-Options"))
	require.Empty(t, resp.Header().Get("Content-Security-Policy"))
	require.Equal(t, "no-referrer", resp.Header().Get("Referrer-Policy"))
	require.Empty(t, resp.Header().Get("Permissions-Policy"))
	require.Equal(t, "require-corp", resp.Header().Get("Cross-Origin-Embedder-Policy"))
	require.Equal(t, "same-origin", resp.Header().Get("Cross-Origin-Opener-Policy"))
	require.Equal(t, "same-origin", resp.Header().Get("Cross-Origin-Resource-Policy"))
	require.Equal(t, "?1", resp.Header().Get("Origin-Agent-Cluster"))
	require.Equal(t, "off", resp.Header().Get("X-DNS-Prefetch-Control"))
	require.Equal(t, "noopen", resp.Header().Get("X-Download-Options"))
	require.Equal(t, "none", resp.Header().Get("X-Permitted-Cross-Domain-Policies"))
}

func Test_CustomValues_AllHeaders(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build(Config{
		// Custom values for all headers
		XSSProtection:             "0",
		ContentTypeNosniff:        "custom-nosniff",
		XFrameOptions:             "DENY",
		HSTSExcludeSubdomains:     true,
		ContentSecurityPolicy:     "default-src 'none'",
		CSPReportOnly:             true,
		ReferrerPolicy:            "origin",
		PermissionPolicy:          "geolocation=(self)",
		CrossOriginEmbedderPolicy: "custom-value",
		CrossOriginOpenerPolicy:   "custom-value",
		CrossOriginResourcePolicy: "custom-value",
		OriginAgentCluster:        "custom-value",
		XDNSPrefetchControl:       "custom-control",
		XDownloadOptions:          "custom-options",
		XPermittedCrossDomain:     "custom-policies",
	}))

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	// Assertions for custom header values
	require.Equal(t, "0", resp.Header().Get("X-XSS-Protection"))
	require.Equal(t, "custom-nosniff", resp.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "DENY", resp.Header().Get("X-Frame-Options"))
	require.Equal(t, "default-src 'none'", resp.Header().Get("Content-Security-Policy-Report-Only"))
	require.Equal(t, "origin", resp.Header().Get("Referrer-Policy"))
	require.Equal(t, "geolocation=(self)", resp.Header().Get("Permissions-Policy"))
	require.Equal(t, "custom-value", resp.Header().Get("Cross-Origin-Embedder-Policy"))
	require.Equal(t, "custom-value", resp.Header().Get("Cross-Origin-Opener-Policy"))
	require.Equal(t, "custom-value", resp.Header().Get("Cross-Origin-Resource-Policy"))
	require.Equal(t, "custom-value", resp.Header().Get("Origin-Agent-Cluster"))
	require.Equal(t, "custom-control", resp.Header().Get("X-DNS-Prefetch-Control"))
	require.Equal(t, "custom-options", resp.Header().Get("X-Download-Options"))
	require.Equal(t, "custom-policies", resp.Header().Get("X-Permitted-Cross-Domain-Policies"))
}

func Test_RealWorldValues_AllHeaders(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build(Config{
		// Real-world values for all headers
		XSSProtection:             "0",
		ContentTypeNosniff:        "nosniff",
		XFrameOptions:             "SAMEORIGIN",
		HSTSExcludeSubdomains:     false,
		ContentSecurityPolicy:     "default-src 'self';base-uri 'self';font-src 'self' https: data:;form-action 'self';frame-ancestors 'self';img-src 'self' data:;object-src 'none';script-src 'self';script-src-attr 'none';style-src 'self' https: 'unsafe-inline';upgrade-insecure-requests",
		CSPReportOnly:             false,
		HSTSPreloadEnabled:        true,
		ReferrerPolicy:            "no-referrer",
		PermissionPolicy:          "geolocation=(self)",
		CrossOriginEmbedderPolicy: "require-corp",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
		OriginAgentCluster:        "?1",
		XDNSPrefetchControl:       "off",
		XDownloadOptions:          "noopen",
		XPermittedCrossDomain:     "none",
	}))

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	// Assertions for real-world header values
	require.Equal(t, "0", resp.Header().Get("X-XSS-Protection"))
	require.Equal(t, "nosniff", resp.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "SAMEORIGIN", resp.Header().Get("X-Frame-Options"))
	require.Equal(t, "default-src 'self';base-uri 'self';font-src 'self' https: data:;form-action 'self';frame-ancestors 'self';img-src 'self' data:;object-src 'none';script-src 'self';script-src-attr 'none';style-src 'self' https: 'unsafe-inline';upgrade-insecure-requests", resp.Header().Get("Content-Security-Policy"))
	require.Equal(t, "no-referrer", resp.Header().Get("Referrer-Policy"))
	require.Equal(t, "geolocation=(self)", resp.Header().Get("Permissions-Policy"))
	require.Equal(t, "require-corp", resp.Header().Get("Cross-Origin-Embedder-Policy"))
	require.Equal(t, "same-origin", resp.Header().Get("Cross-Origin-Opener-Policy"))
	require.Equal(t, "same-origin", resp.Header().Get("Cross-Origin-Resource-Policy"))
	require.Equal(t, "?1", resp.Header().Get("Origin-Agent-Cluster"))
	require.Equal(t, "off", resp.Header().Get("X-DNS-Prefetch-Control"))
	require.Equal(t, "noopen", resp.Header().Get("X-Download-Options"))
	require.Equal(t, "none", resp.Header().Get("X-Permitted-Cross-Domain-Policies"))
}

func Test_Next(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build(Config{
		Next: func(ctx *gin.Context) bool {
			return ctx.Request.URL.Path == "/next"
		},
		ReferrerPolicy: "no-referrer",
	}))

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})
	app.GET("/next", func(c *gin.Context) {
		c.String(http.StatusOK, "Skipped!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, "no-referrer", resp.Header().Get("Referrer-Policy"))

	resp = serve(app, httptest.NewRequest(http.MethodGet, "/next", http.NoBody))
	require.Empty(t, resp.Header().Get("Referrer-Policy"))
}

func Test_ContentSecurityPolicy(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build(Config{
		ContentSecurityPolicy: "default-src 'none'",
	}))

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, "default-src 'none'", resp.Header().Get("Content-Security-Policy"))
}

func Test_ContentSecurityPolicyReportOnly(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build(Config{
		ContentSecurityPolicy: "default-src 'none'",
		CSPReportOnly:         true,
	}))

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, "default-src 'none'", resp.Header().Get("Content-Security-Policy-Report-Only"))
	require.Empty(t, resp.Header().Get("Content-Security-Policy"))
}

func Test_PermissionsPolicy(t *testing.T) {
	t.Parallel()
	app := gin.New()

	app.Use(NewBuilder().Build(Config{
		PermissionPolicy: "microphone=()",
	}))

	app.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Hello, World!")
	})

	resp := serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	require.Equal(t, "microphone=()", resp.Header().Get("Permissions-Policy"))
}

func Test_HSTSHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scheme   string
		expected string
		config   Config
	}{
		{
			name:     "TLS request with max age sets header",
			config:   Config{HSTSMaxAge: 60},
			scheme:   "https",
			expected: "max-age=60; includeSubDomains",
		},
		{
			name:     "TLS request with excluded subdomains sets bare max-age",
			config:   Config{HSTSMaxAge: 60, HSTSExcludeSubdomains: true},
			scheme:   "https",
			expected: "max-age=60",
		},
		{
			name:   "TLS request with zero max age omits header",
			config: Config{HSTSMaxAge: 0},
			scheme: "https",
		},
		{
			name:   "HTTP request with max age omits header",
			config: Config{HSTSMaxAge: 60},
			scheme: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.expected, hstsHeaderForRequest(t, &tt.config, tt.scheme))
		})
	}
}

func Test_HSTSHeadersPanicsOnNegativeMaxAge(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "helmet: HSTSMaxAge must be greater than or equal to 0", func() {
		NewBuilder().Build(Config{HSTSMaxAge: -1})
	})
}

func Test_HSTSExcludeSubdomainsAndPreload(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "helmet: HSTSPreloadEnabled requires HSTSExcludeSubdomains to be false", func() {
		NewBuilder().Build(Config{
			HSTSMaxAge:            31536000,
			HSTSExcludeSubdomains: true,
			HSTSPreloadEnabled:    true,
		})
	})
}

func Test_HSTSPreloadIncludesSubdomains(t *testing.T) {
	t.Parallel()

	require.Equal(t, "max-age=31536000; includeSubDomains; preload", hstsHeaderForRequest(t, &Config{
		HSTSMaxAge:         31536000,
		HSTSPreloadEnabled: true,
	}, "https"))
}

var helmetHeaders = []string{
	"X-XSS-Protection", "X-Content-Type-Options", "X-Frame-Options",
	"Cross-Origin-Embedder-Policy", "Cross-Origin-Opener-Policy", "Cross-Origin-Resource-Policy",
	"Origin-Agent-Cluster", "Referrer-Policy", "X-DNS-Prefetch-Control", "X-Download-Options",
	"X-Permitted-Cross-Domain-Policies", "Strict-Transport-Security", "Content-Security-Policy",
	"Content-Security-Policy-Report-Only", "Permissions-Policy",
}

func routerWithHelmet(handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(handler)
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "OK") })
	return router
}

func TestEmptyConfigUsesUpstreamDefaults(t *testing.T) {
	t.Parallel()
	var builder Builder // The zero value is usable and retains no configuration.
	defaults := serve(routerWithHelmet(builder.Build()), httptest.NewRequest("GET", "/", nil))
	empty := serve(routerWithHelmet(builder.Build(Config{})), httptest.NewRequest("GET", "/", nil))
	require.Equal(t, defaults.Header(), empty.Header())
	require.Equal(t, "OK", empty.Body.String())
}

func TestOnlyFirstConfigIsApplied(t *testing.T) {
	t.Parallel()
	middleware := NewBuilder().Build(Config{XFrameOptions: "DENY"}, Config{XFrameOptions: "ignored", HSTSMaxAge: -1})
	response := serve(routerWithHelmet(middleware), httptest.NewRequest("GET", "/", nil))
	require.Equal(t, "DENY", response.Header().Get("X-Frame-Options"))
	require.Empty(t, response.Header().Get("Strict-Transport-Security"))
}

func TestConfigSnapshotAndHeadersBeforeNext(t *testing.T) {
	t.Parallel()
	cfg := Config{
		XFrameOptions: "DENY", HSTSMaxAge: 60,
		ContentSecurityPolicy: "default-src 'none'",
		IsSecure:              func(*gin.Context) bool { return true },
	}
	middleware := NewBuilder().Build(cfg)
	cfg.XFrameOptions = "changed"
	cfg.HSTSMaxAge = 120
	cfg.IsSecure = func(*gin.Context) bool { return false }
	cfg.Next = func(*gin.Context) bool { return true }
	router := gin.New()
	router.Use(middleware)
	calls := 0
	router.GET("/", func(c *gin.Context) {
		calls++
		require.Equal(t, "DENY", c.Writer.Header().Get("X-Frame-Options"))
		require.Equal(t, "max-age=60; includeSubDomains", c.Writer.Header().Get("Strict-Transport-Security"))
		require.Equal(t, "default-src 'none'", c.Writer.Header().Get("Content-Security-Policy"))
		c.String(http.StatusTeapot, "tea")
	})
	response := serve(router, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 1, calls)
	require.Equal(t, http.StatusTeapot, response.Code)
	require.Equal(t, "tea", response.Body.String())
}

func TestSkipBypassesAllHeadersAndSecureCallback(t *testing.T) {
	t.Parallel()
	middleware := NewBuilder().Build(Config{
		Next:       func(*gin.Context) bool { return true },
		HSTSMaxAge: 60, ContentSecurityPolicy: "default-src 'none'", PermissionPolicy: "camera=()",
		IsSecure: func(*gin.Context) bool { t.Fatal("skipped request checked security"); return false },
	})
	response := serve(routerWithHelmet(middleware), httptest.NewRequest("GET", "/", nil))
	require.Equal(t, 200, response.Code)
	require.Equal(t, "OK", response.Body.String())
	for _, header := range helmetHeaders {
		require.Empty(t, response.Header().Get(header), header)
	}
}

func TestDefaultSecureCheckIgnoresSpoofedScheme(t *testing.T) {
	t.Parallel()
	middleware := NewBuilder().Build(Config{HSTSMaxAge: 60})
	request := httptest.NewRequest("GET", "https://example.com/", nil)
	request.TLS = nil // An absolute URL does not establish a TLS connection.
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Protocol", "https")
	request.Header.Set("X-Forwarded-Ssl", "on")
	request.Header.Set("X-Url-Scheme", "https")
	response := serve(routerWithHelmet(middleware), request)
	require.Empty(t, response.Header().Get("Strict-Transport-Security"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
}

func TestCustomSecureCheckForTrustedProxy(t *testing.T) {
	t.Parallel()
	middleware := NewBuilder().Build(Config{HSTSMaxAge: 60, IsSecure: func(c *gin.Context) bool {
		if c.Request.TLS != nil {
			return true
		}
		if c.RemoteIP() != "10.0.0.1" {
			return false
		}
		proto, _, _ := strings.Cut(c.GetHeader("X-Forwarded-Proto"), ",")
		return strings.EqualFold(strings.TrimSpace(proto), "https")
	}})
	for _, tc := range []struct{ name, remote, scheme, expected string }{
		{"trusted HTTPS", "10.0.0.1:1234", "https", "max-age=60; includeSubDomains"},
		{"first forwarded scheme", "10.0.0.1:1234", " HTTPS , http", "max-age=60; includeSubDomains"},
		{"trusted HTTP", "10.0.0.1:1234", "http", ""},
		{"untrusted spoof", "203.0.113.1:1234", "https", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = tc.remote
			request.Header.Set("X-Forwarded-Proto", tc.scheme)
			response := serve(routerWithHelmet(middleware), request)
			require.Equal(t, tc.expected, response.Header().Get("Strict-Transport-Security"))
		})
	}
}

func TestUnconfiguredHeadersArePreserved(t *testing.T) {
	t.Parallel()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("Content-Security-Policy", "existing-enforcement")
		c.Header("Strict-Transport-Security", "existing-hsts")
		c.Header("X-Frame-Options", "existing-frame-policy")
		c.Next()
	}, NewBuilder().Build(Config{ContentSecurityPolicy: "report-policy", CSPReportOnly: true}))
	router.GET("/", func(c *gin.Context) { c.Status(204) })
	response := serve(router, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, "existing-enforcement", response.Header().Get("Content-Security-Policy"))
	require.Equal(t, "report-policy", response.Header().Get("Content-Security-Policy-Report-Only"))
	require.Equal(t, "existing-hsts", response.Header().Get("Strict-Transport-Security"))
	require.Equal(t, "SAMEORIGIN", response.Header().Get("X-Frame-Options"))
}

func TestConfiguredZeroMaxAgeDoesNotEmitHSTS(t *testing.T) {
	t.Parallel()
	middleware := NewBuilder().Build(Config{
		HSTSPreloadEnabled: true,
		IsSecure:           func(*gin.Context) bool { t.Fatal("disabled HSTS consulted secure callback"); return true },
	})
	response := serve(routerWithHelmet(middleware), httptest.NewRequest("GET", "/", nil))
	require.Empty(t, response.Header().Get("Strict-Transport-Security"))
}

func hstsHeaderForRequest(t *testing.T, config *Config, scheme string) string {
	t.Helper()
	app := gin.New()
	app.Use(NewBuilder().Build(*config))
	app.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "Hello, World!") })
	if scheme == "http" {
		return serve(app, httptest.NewRequest(http.MethodGet, "/", http.NoBody)).Header().Get("Strict-Transport-Security")
	}
	require.Equal(t, "https", scheme)
	server := httptest.NewTLSServer(app)
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL + "/")
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	return response.Header.Get("Strict-Transport-Security")
}

func serve(app http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	return response
}
