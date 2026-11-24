package ginx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyHandleForwardsToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("upstream"))
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", ProxyHandle(func(*Context) (*httputil.ReverseProxy, error) {
		return proxy, nil
	}))

	// A real server is required: gin's responseWriter panics when the
	// underlying recorder lacks http.CloseNotifier, which ReverseProxy uses.
	// 必须用真实服务器：recorder 缺少 http.CloseNotifier 会让 gin 的
	// responseWriter 在 ReverseProxy 调用 CloseNotify 时 panic。
	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/x")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "upstream", string(body))
}

func TestProxyHandleAbortsWith502(t *testing.T) {
	tests := []struct {
		name  string
		proxy *httputil.ReverseProxy
		err   error
	}{
		{name: "error", err: errors.New("no upstream")},
		{name: "nil proxy without error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.GET("/x", ProxyHandle(func(*Context) (*httputil.ReverseProxy, error) {
				return tt.proxy, tt.err
			}))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			assert.Equal(t, http.StatusBadGateway, w.Code)
			assert.Equal(t, http.StatusText(http.StatusBadGateway), w.Body.String(),
				"internal errors stay out of the response body")
		})
	}
}
