package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/auth"
	"github.com/gin-gonic/gin"
)

func TestWhitelistAndProtectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		method     string
		path       string
		authHeader string
		want       int
	}{
		{name: "login is public", method: http.MethodPost, path: "/login", want: http.StatusOK},
		{name: "profile requires auth", method: http.MethodGet, path: "/profile", want: http.StatusUnauthorized},
		{name: "invalid token", method: http.MethodGet, path: "/profile", authHeader: "Bearer wrong", want: http.StatusUnauthorized},
		{name: "valid token", method: http.MethodGet, path: "/profile", authHeader: "Bearer demo-token", want: http.StatusOK},
		{name: "exact whitelist does not match suffix", method: http.MethodPost, path: "/admin/login-history", want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			example.New().ServeHTTP(r, req)
			if r.Code != tt.want {
				t.Fatalf("want %d for %s %s, got %d", tt.want, tt.method, tt.path, r.Code)
			}
		})
	}
}
