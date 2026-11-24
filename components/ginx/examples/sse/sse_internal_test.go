package sse

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/ginx"
)

// failingWriter breaks one specific step while delegating everything else to
// gin's writer: FlushError fails NewWriter, Write failures simulate a client
// that disconnected after failAfter successful writes.
// failingWriter 只破坏指定环节，其余委托给 gin 的 writer：FlushError 让
// NewWriter 失败，Write 在 failAfter 次成功写入后模拟客户端断开。
type failingWriter struct {
	gin.ResponseWriter
	failAfter int
	writes    int
	writeErr  error
	flushErr  error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.writes >= w.failAfter {
		return 0, w.writeErr
	}
	w.writes++
	return w.ResponseWriter.Write(p)
}

func (w *failingWriter) FlushError() error { return w.flushErr }

func failingContext(t *testing.T, fw *failingWriter) *ginx.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	fw.ResponseWriter = c.Writer
	c.Writer = fw
	c.Request = httptest.NewRequest(http.MethodGet, "/events/lobby?count=2", nil)
	return &ginx.Context{Context: c}
}

func TestStreamEventStreamFailure(t *testing.T) {
	fw := &failingWriter{flushErr: errors.New("flush broke")}
	_, err := stream(failingContext(t, fw))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ginx.ErrNoResponse, "an opening failure is a real error, not a finished stream")
}

func TestStreamCommentFailure(t *testing.T) {
	fw := &failingWriter{writeErr: errors.New("client gone")}
	_, err := stream(failingContext(t, fw))
	require.ErrorIs(t, err, ginx.ErrNoResponse)
}

func TestStreamSendFailure(t *testing.T) {
	// failAfter=1 lets the opening comment through; the first event fails.
	// failAfter=1 放行探活注释，第一个事件写入失败。
	fw := &failingWriter{failAfter: 1, writeErr: errors.New("client gone")}
	_, err := stream(failingContext(t, fw))
	require.ErrorIs(t, err, ginx.ErrNoResponse)
	assert.GreaterOrEqual(t, fw.writes, 1, "the comment must have been written before the failure")
}
