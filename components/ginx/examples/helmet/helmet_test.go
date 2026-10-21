package helmet_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/helmet"
	"github.com/gin-gonic/gin"
)

func TestSecurityHeadersPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	helmet.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	// The upstream default config sets X-XSS-Protection ("0" is the modern
	// recommended value); presence is what matters here.
	if w.Header().Get("X-XSS-Protection") == "" {
		t.Fatal("helmet headers missing")
	}
}
