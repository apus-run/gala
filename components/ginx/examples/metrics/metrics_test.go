package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/metrics"
	"github.com/gin-gonic/gin"
)

func TestScrapeExposesRequestCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := example.New()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 from /metrics, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "http_request_count_total") {
		t.Fatal("request counter missing from scrape")
	}
}
