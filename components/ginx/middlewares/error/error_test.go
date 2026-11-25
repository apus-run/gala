package error

import (
	"context"
	stderrs "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/apus-run/gala/pkg/errcode"
)

func TestBuilder_Build_WithAppError(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(NewBuilder().Build())
	r.GET("/test", func(c *gin.Context) {
		_ = c.Error(errcode.ErrInvalidArgument.
			WithMessage("invalid input").
			KV("field", "name"))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, errcode.ContentType, w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.JSONEq(t, `{
		"title":"Bad Request",
		"status":400,
		"detail":"invalid input",
		"reason":"InvalidArgument",
		"metadata":{"field":"name"}
	}`, w.Body.String())
}

func TestBuilder_Build_WithStdError(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(NewBuilder().Build())
	r.GET("/test", func(c *gin.Context) {
		_ = c.Error(stderrs.New("db down"))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	// A plain error is an internal failure; the cause stays out of the body.
	// 普通错误归为内部故障，底层原因不进入响应体。
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{
		"title":"Internal Server Error",
		"status":500,
		"detail":"Internal server error.",
		"reason":"InternalError"
	}`, w.Body.String())
}

func TestBuilder_Build_CanceledContext(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(NewBuilder().Build())
	r.GET("/test", func(c *gin.Context) {
		_ = c.Error(context.Canceled)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	assert.Equal(t, 499, w.Code)
	assert.JSONEq(t, `{
		"status":499,
		"detail":"Request canceled.",
		"reason":"Canceled"
	}`, w.Body.String())
}

func TestBuilder_Build_DoNotOverrideWrittenResponse(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(NewBuilder().Build())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusTeapot, gin.H{"message": "already written"})
		_ = c.Error(errcode.ErrInternal.WithMessage("ignored"))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))

	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.JSONEq(t, `{"message":"already written"}`, w.Body.String())
}
