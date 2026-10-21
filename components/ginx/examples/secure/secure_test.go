package secure_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/secure"
	"github.com/gin-gonic/gin"
)

func TestPlainHTTPRedirectsToSSLHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	secure.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("want 301 redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc == "" {
		t.Fatal("redirect location missing")
	}
}
