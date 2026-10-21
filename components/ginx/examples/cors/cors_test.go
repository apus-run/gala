package cors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/cors"
	"github.com/gin-gonic/gin"
)

func preflight(r *gin.Engine, origin string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/orders", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	r.ServeHTTP(w, req)
	return w
}

func TestPreflightFromAllowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := cors.New()
	w := preflight(r, "https://app.example.com")
	if w.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("allowed origin not echoed: %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if a := w.Header().Get("Access-Control-Allow-Methods"); a == "" {
		t.Fatal("allow-methods missing on preflight")
	}
}

func TestPreflightFromUnknownOriginGetsNoEcho(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := preflight(cors.New(), "https://evil.example.net")
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unknown origin must not be echoed: %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}
