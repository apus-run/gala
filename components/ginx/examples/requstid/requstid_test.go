package requstid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/requstid"
	"github.com/gin-gonic/gin"
)

func get(r *gin.Engine, path string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestGeneratedIDEchoed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := get(requstid.New(), "/whoami", nil)
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("generated request id missing from response header")
	}
}

func TestIncomingIDHonored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := get(requstid.New(), "/whoami", map[string]string{"X-Request-ID": "trace-42"})
	if w.Header().Get("X-Request-ID") != "trace-42" {
		t.Fatalf("incoming id not honored: %s", w.Header().Get("X-Request-ID"))
	}
}
