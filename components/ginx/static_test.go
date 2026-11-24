package ginx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupEmbedAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	r := gin.New()
	SetupEmbedAssets(r.Group("/static"), http.FS(assets), "assets")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/assets/app.js", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "console.log(1)", w.Body.String())

	// HEAD is registered next to GET for every path.
	// 每条路径都在 GET 之外注册了 HEAD。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/static/assets/app.js", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestSetupStaticAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "app.txt"), []byte("asset"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>home</h1>"), 0o644))

	r := gin.New()
	require.NoError(t, SetupStaticAssets(r.Group("/"), dir))

	// Top-level subdirectories become routes; index.html serves the root.
	// 顶层子目录成为路由；index.html 服务根路径。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app.txt", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "asset", w.Body.String())

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "<h1>home</h1>", w.Body.String())
}

func TestSetupStaticAssetsReturnsWalkError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	err := SetupStaticAssets(gin.New().Group("/"), filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err, "a missing directory must surface instead of killing the process")
}
