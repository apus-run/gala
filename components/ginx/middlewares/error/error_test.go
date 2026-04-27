package error

import (
	stderrs "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/apus-run/gala/pkg/errorsx"
)

func TestBuilder_Build_WithErrorsxError(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(NewBuilder().Build())
	r.GET("/test", func(c *gin.Context) {
		err := errorsx.InvalidParams("INVALID_PARAMS").
			WithMessage("invalid input").
			KV("field", "name")
		_ = c.Error(err)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{
		"code":400,
		"status":"INVALID_PARAMS",
		"message":"invalid input",
		"details":{"field":"name"}
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
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{
		"code":500,
		"status":"InternalError",
		"message":"db down"
	}`, w.Body.String())
}

func TestBuilder_Build_DoNotOverrideWrittenResponse(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(NewBuilder().Build())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusTeapot, gin.H{"message": "already written"})
		_ = c.Error(errorsx.InternalServer("INTERNAL").WithMessage("ignored"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.JSONEq(t, `{"message":"already written"}`, w.Body.String())
}
