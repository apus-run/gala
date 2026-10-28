package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/authn"
)

func testAuthFunc(_ context.Context, r *http.Request) (*authn.SessionClaims, error) {
	token, ok := authn.BearerToken(r)
	if !ok {
		return nil, authn.ErrMissingToken
	}
	if token == "storage-down" {
		return nil, errors.New("storage unavailable")
	}
	if token != "good" {
		return nil, authn.ErrTokenInvalid
	}
	return &authn.SessionClaims{}, nil
}

func TestBuilderWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewBuilder(testAuthFunc).
		IgnorePaths("/login").
		IgnorePrefix("/public").
		Build())
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	r.POST("/login", ok)
	r.GET("/admin/login-history", ok)
	r.GET("/public", ok)
	r.GET("/public/:name", ok)
	r.GET("/publicity", ok)

	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "exact path", path: "/login", want: http.StatusNoContent},
		{name: "exact path does not match suffix", path: "/admin/login-history", want: http.StatusUnauthorized},
		{name: "prefix path itself", path: "/public", want: http.StatusNoContent},
		{name: "prefix child", path: "/public/a", want: http.StatusNoContent},
		{name: "prefix requires segment boundary", path: "/publicity", want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodGet
			if tt.path == "/login" {
				method = http.MethodPost
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, tt.path, nil))
			if w.Code != tt.want {
				t.Fatalf("want %d for %s, got %d", tt.want, tt.path, w.Code)
			}
		})
	}
}

func TestBuilderAuthenticationAndIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewBuilder(testAuthFunc).Build())
	r.GET("/profile", func(c *gin.Context) {
		claims, ok := authn.FromContext[*authn.SessionClaims](c.Request.Context())
		if !ok || claims == nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	tests := []struct {
		name      string
		header    string
		want      int
		challenge string
	}{
		{name: "missing", want: http.StatusUnauthorized, challenge: "Bearer"},
		{name: "invalid", header: "Bearer bad", want: http.StatusUnauthorized, challenge: `Bearer error="invalid_token"`},
		{name: "storage failure", header: "Bearer storage-down", want: http.StatusServiceUnavailable},
		{name: "valid", header: "Bearer good", want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/profile", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("want %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if got := w.Header().Get("WWW-Authenticate"); got != tt.challenge {
				t.Fatalf("want challenge %q, got %q", tt.challenge, got)
			}
		})
	}
}
