package cost_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/cost"
	"github.com/gin-gonic/gin"
)

func TestRequestPassesAndIsServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	cost.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("unexpected response: %d %q", w.Code, w.Body.String())
	}
}
