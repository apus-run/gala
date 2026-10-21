package recovery_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/recovery"
	"github.com/gin-gonic/gin"
)

func TestPanicBecomes500HealthyPathUnaffected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := recovery.New()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders/0", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 from recovered panic, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders/7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 on the healthy path, got %d", w.Code)
	}
}
