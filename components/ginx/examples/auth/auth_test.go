package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/auth"
	"github.com/gin-gonic/gin"
)

// Documents today's behavior: the whitelist and the rest both pass until the
// token check is implemented upstream. Flip these expectations when it lands.
func TestWhitelistAndProfileBothPassToday(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := example.New()
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/login"},
		{http.MethodGet, "/profile"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("want 200 for %s %s, got %d", tc.method, tc.path, w.Code)
		}
	}
}
