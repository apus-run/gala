package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// A process may mount Metrics on more than one engine; the second call must
// not panic on duplicate registration nor leak another uptime goroutine.
func TestMetricsRegistrablePerEngine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for range 2 {
		r := gin.New()
		r.Use(Metrics(r))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("want 200 from /metrics, got %d", w.Code)
		}
	}
}
