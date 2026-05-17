package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpServer "github.com/apus-run/gala/server/http"
)

func TestNewServer(t *testing.T) {
	baseURL, _ := startTestServer(t)

	assert.Eventually(t, func() bool {
		resp, err := http.Get(baseURL)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second, 10*time.Millisecond)
}

func TestServer(t *testing.T) {
	baseURL, srv := startTestServer(t)

	resp, err := http.Get(baseURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, srv.Stop(stopCtx))
}

func TestServerWG(t *testing.T) {
	_, srv := startTestServer(t)

	done := make(chan error, 1)
	go func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- srv.Stop(stopCtx)
	}()

	require.NoError(t, <-done)
}

func TestWithHandlerNilKeepsDefaultHandler(t *testing.T) {
	srv := httpServer.NewServer(httpServer.WithHandler(nil))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		srv.ServeHTTP(recorder, req)
	})
}

func startTestServer(t *testing.T) (string, *httpServer.Server) {
	t.Helper()

	g := gin.New()
	g.Handle(http.MethodGet, "/", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	srv := httpServer.NewServer(
		httpServer.WithHandler(g),
		httpServer.WithAddress("127.0.0.1:0"),
	)
	endpoint, err := srv.Endpoint()
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(t.Context())
	}()

	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Stop(stopCtx)

		select {
		case err := <-errCh:
			assert.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("server did not stop")
		}
	})

	return endpoint.String(), srv
}
