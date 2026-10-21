package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/ratelimit"
	"github.com/gin-gonic/gin"
)

func TestBurstPastWindowRefused(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := example.New()
	codes := make([]int, 3)
	for i := range codes {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/search", nil))
		codes[i] = w.Code
	}
	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("first two should pass, got %v", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("third should be refused, got %v", codes)
	}
}
