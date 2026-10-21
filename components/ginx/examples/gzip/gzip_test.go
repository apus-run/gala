package gzip_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/gzip"
	"github.com/gin-gonic/gin"
)

func TestCompressedForAskingClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gzip.New()

	req := httptest.NewRequest(http.MethodGet, "/report", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzip content-encoding, got %q", w.Header().Get("Content-Encoding"))
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/report", nil)) // no Accept-Encoding
	if w.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("must not compress for plain clients")
	}
}
