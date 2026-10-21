package tracing_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/tracing"
	"github.com/gin-gonic/gin"
)

func TestNoopProviderKeepsRequestsWorking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	example.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}
