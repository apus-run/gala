package static_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/static"
	"github.com/gin-gonic/gin"
)

func TestServesExistingFileAndFallsThroughOtherwise(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := os.MkdirAll("static", 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll("static")
	if err := os.WriteFile(filepath.Join("static", "index.html"), []byte("<h1>gala</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := example.New()
	// http.FileServer redirects /index.html to ./, so ask for the directory
	// root and let it serve index.html directly.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || w.Body.String() != "<h1>gala</h1>" {
		t.Fatalf("static file not served: %d %q", w.Code, w.Body.String())
	}
	// A missing file does not abort: the request falls through to routes.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("api route should still work, got %d", w.Code)
	}
}
