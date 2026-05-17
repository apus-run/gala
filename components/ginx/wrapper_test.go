package ginx

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBind(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewBufferString(`{"title":"我是标题", "email":"@163.com"}`))
	c.Request.Header.Add("Content-Type", gin.MIMEJSON)
	gc := Context{Context: c}
	var obj struct {
		Title string  `json:"title"`
		Email *string `json:"email"`
	}
	t.Log("Bind:", gc.Bind(&obj))
	assert.Equal(t, w.Code, 200)
	t.Log("Code:", w.Code, "Body:", w.Body.String())
	assert.Empty(t, c.Errors)
}

func TestShouldBind(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewBufferString(`{"title":"t", "email":"@shimo.im"}`))
	c.Request.Header.Add("Content-Type", gin.MIMEJSON)
	gc := Context{Context: c}
	var obj struct {
		Title string  `json:"title"`
		Email *string `json:"email"`
	}

	t.Log("Bind:", gc.ShouldBind(&obj))
	assert.Equal(t, w.Code, 200)
	t.Log("Code:", w.Code, "Body:", w.Body.String())
	assert.Empty(t, c.Errors)
}

func TestContext_Query(t *testing.T) {
	testCases := []struct {
		name    string
		req     func(t *testing.T) *http.Request
		key     string
		wantVal any
	}{
		{
			name: "获得数据",
			req: func(t *testing.T) *http.Request {
				req, err := http.NewRequest(http.MethodGet, "http://localhost/abc?name=123&age=18", nil)
				require.NoError(t, err)
				return req
			},
			key:     "name",
			wantVal: "123",
		},
		{
			name: "没有数据",
			req: func(t *testing.T) *http.Request {
				req, err := http.NewRequest(http.MethodGet, "http://localhost/abc?name=123&age=18", nil)
				require.NoError(t, err)
				return req
			},
			key:     "nickname",
			wantVal: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{Context: &gin.Context{
				Request: tc.req(t),
			}}
			val := ctx.Query(tc.key)
			assert.Equal(t, tc.wantVal, val.StringOrDefault(""))
		})
	}
}

func TestContext_Param(t *testing.T) {
	testCases := []struct {
		name    string
		req     func(t *testing.T) *http.Request
		key     string
		wantVal any
	}{
		{
			name: "获得数据",
			req: func(t *testing.T) *http.Request {
				req, err := http.NewRequest(http.MethodGet, "http://localhost/hello/world", nil)
				require.NoError(t, err)
				return req
			},
			key:     "name",
			wantVal: "world",
		},
		{
			name: "没有数据",
			req: func(t *testing.T) *http.Request {
				req, err := http.NewRequest(http.MethodPost, "http://localhost/hello", nil)
				require.NoError(t, err)
				return req
			},
			key:     "nickname",
			wantVal: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := gin.New()
			server.GET("/hello/:name", func(context *gin.Context) {
				ctx := &Context{Context: context}
				val := ctx.Param(tc.key)
				assert.Equal(t, tc.wantVal, val.StringOrDefault(""))
			})
			server.POST("/hello", func(context *gin.Context) {
				ctx := &Context{Context: context}
				val := ctx.Param(tc.key)
				assert.Equal(t, tc.wantVal, val.StringOrDefault(""))
			})
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, tc.req(t))
		})
	}
}

func TestContext_Cookie(t *testing.T) {
	testCases := []struct {
		name    string
		req     func(t *testing.T) *http.Request
		key     string
		wantVal any
	}{
		{
			name: "有cookie",
			req: func(t *testing.T) *http.Request {
				req, err := http.NewRequest(http.MethodGet, "http://localhost/hello?name=123&age=18", nil)
				req.AddCookie(&http.Cookie{
					Name:  "name",
					Value: "world",
				})
				require.NoError(t, err)
				return req
			},
			key:     "name",
			wantVal: "world",
		},
		{
			name: "没有 cookie",
			req: func(t *testing.T) *http.Request {
				req, err := http.NewRequest(http.MethodPost, "http://localhost/hello?name=123&age=18", nil)
				require.NoError(t, err)
				return req
			},
			key:     "nickname",
			wantVal: "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := gin.New()
			server.Any("/hello", func(context *gin.Context) {
				ctx := &Context{Context: context}
				val := ctx.Cookie(tc.key)
				assert.Equal(t, tc.wantVal, val.StringOrDefault(""))
			})
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, tc.req(t))
		})
	}
}

func TestGinContextHelpersHandleNilContext(t *testing.T) {
	ctx := NewGinContext(nil, nil)
	require.NotNil(t, ctx)

	ginCtx, ok := FromGinContext(nil)
	assert.False(t, ok)
	assert.Nil(t, ginCtx)
}

func TestNewGinContextStoresGinContext(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx := NewGinContext(context.Background(), c)

	got, ok := FromGinContext(ctx)

	assert.True(t, ok)
	assert.Same(t, c, got)
}

func TestContext_GetRequestIdIgnoresNonString(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctx := &Context{Context: c}

	ctx.Set(requestIdFieldKey, 123)

	assert.Equal(t, "", ctx.GetRequestId())
}

func TestBInvalidRequestReturnsJSONBadRequest(t *testing.T) {
	called := false
	handler := B(func(ctx *Context, req struct {
		Name string `json:"name" binding:"required"`
	}) (Result, error) {
		called = true
		return Result{Code: CodeOK}, nil
	})
	server := gin.New()
	server.POST("/bind", handler)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/bind", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", gin.MIMEJSON)
	server.ServeHTTP(recorder, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, recorder.Body.String(), `"code":400`)
}

func TestWCErrorDoesNotReturnOK(t *testing.T) {
	server := gin.New()
	server.GET("/claims", func(ctx *gin.Context) {
		ctx.Set("claims", func() jwt.Claims { return jwt.MapClaims{} })
		WC(func(*gin.Context, func() jwt.Claims) (Result, error) {
			return Result{Code: CodeErr, Msg: "failed"}, errors.New("boom")
		})(ctx)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	server.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "failed")
}

func TestBCErrorDoesNotReturnOK(t *testing.T) {
	server := gin.New()
	server.POST("/claims", func(ctx *gin.Context) {
		ctx.Set("claims", func() jwt.Claims { return jwt.MapClaims{} })
		BC(func(*gin.Context, struct{}, func() jwt.Claims) (Result, error) {
			return Result{Code: CodeErr, Msg: "failed"}, errors.New("boom")
		})(ctx)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/claims", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", gin.MIMEJSON)
	server.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "failed")
}
