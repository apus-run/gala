package ginx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/pkg/errorsx"
)

// statusErr carries an HTTP status through the httpStatusError interface.
// statusErr 通过 httpStatusError 接口携带 HTTP 状态码。
type statusErr struct{ code int }

func (e statusErr) Error() string   { return "status error" }
func (e statusErr) HTTPStatus() int { return e.code }

func serve(t *testing.T, pattern, target, body string, h gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET(pattern, h)
	r.POST(pattern, h)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestW(t *testing.T) {
	ok := Result{Code: CodeOK, Msg: "ok", Data: gin.H{"id": 1}}
	tests := []struct {
		name       string
		err        error
		res        Result
		wantStatus int
		wantBody   string
	}{
		{name: "success", res: ok, wantStatus: http.StatusOK, wantBody: `"msg":"ok"`},
		{name: "no response writes nothing", err: ErrNoResponse, wantStatus: http.StatusOK, wantBody: ""},
		{name: "wrapped no response", err: errors.Join(errors.New("x"), ErrNoResponse), wantStatus: http.StatusOK, wantBody: ""},
		{name: "unauthorized", err: ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantBody: ""},
		{name: "error keeps res", err: errors.New("boom"), res: Result{Code: CodeErr, Msg: "failed"},
			wantStatus: http.StatusInternalServerError, wantBody: `"msg":"failed"`},
		{name: "status error", err: statusErr{http.StatusTeapot}, res: Result{Code: CodeErr, Msg: "failed"},
			wantStatus: http.StatusTeapot, wantBody: `"msg":"failed"`},
		{name: "errorsx code", err: errorsx.InvalidParams("BAD"), res: Result{Code: CodeErr, Msg: "failed"},
			wantStatus: http.StatusBadRequest, wantBody: `"msg":"failed"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(t, "/", "/", "", W(func(*Context) (Result, error) { return tt.res, tt.err }))
			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantBody == "" {
				assert.Empty(t, w.Body.String())
			} else {
				assert.Contains(t, w.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestB(t *testing.T) {
	type req struct {
		Name string `json:"name" binding:"required"`
	}
	t.Run("binds and returns", func(t *testing.T) {
		w := serve(t, "/", "/", `{"name":"gala"}`, B(func(_ *Context, r req) (Result, error) {
			return Result{Code: CodeOK, Data: r.Name}, nil
		}))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "gala")
	})

	t.Run("binding failure is 400", func(t *testing.T) {
		w := serve(t, "/", "/", `{}`, B(func(*Context, req) (Result, error) {
			t.Fatal("handler must not run on a binding failure")
			return Result{}, nil
		}))
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), `"code":400`)
	})

	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "no response", err: ErrNoResponse, wantStatus: http.StatusOK},
		{name: "unauthorized", err: ErrUnauthorized, wantStatus: http.StatusUnauthorized},
		{name: "generic error", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(t, "/", "/", `{"name":"gala"}`, B(func(*Context, req) (Result, error) {
				return Result{Code: CodeErr, Msg: "m"}, tt.err
			}))
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestSetAndGetClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/", func(c *gin.Context) {
		_, ok := GetClaims(c)
		assert.False(t, ok, "no claims before SetClaims")

		claims := func() jwt.Claims { return jwt.MapClaims{"sub": "42"} }
		SetClaims(c, claims)
		got, ok := GetClaims(c)
		require.True(t, ok)
		sub, err := got().(jwt.MapClaims).GetSubject()
		require.NoError(t, err)
		assert.Equal(t, "42", sub)

		// A wrong type is reported as absent, not panicked on.
		// 类型不符时报告缺失，而不是 panic。
		c.Set(claimsContextKey{}, "not-a-factory")
		_, ok = GetClaims(c)
		assert.False(t, ok)
	})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestWC(t *testing.T) {
	handler := func(*gin.Context, func() jwt.Claims) (Result, error) {
		return Result{Code: CodeOK, Data: "ok"}, nil
	}

	t.Run("missing claims", func(t *testing.T) {
		w := serve(t, "/", "/", "", WC(handler))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "success", wantStatus: http.StatusOK},
		{name: "no response", err: ErrNoResponse, wantStatus: http.StatusOK},
		{name: "unauthorized", err: ErrUnauthorized, wantStatus: http.StatusUnauthorized},
		{name: "generic error", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(t, "/", "/", "", withClaims(WC(func(*gin.Context, func() jwt.Claims) (Result, error) {
				return Result{Code: CodeErr}, tt.err
			})))
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestBC(t *testing.T) {
	type req struct {
		Name string `json:"name" binding:"required"`
	}
	handler := func(*gin.Context, req, func() jwt.Claims) (Result, error) {
		return Result{Code: CodeOK}, nil
	}

	t.Run("binding failure", func(t *testing.T) {
		w := serve(t, "/", "/", `{}`, withClaims(BC(handler)))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing claims", func(t *testing.T) {
		w := serve(t, "/", "/", `{"name":"x"}`, BC(handler))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "success", wantStatus: http.StatusOK},
		{name: "no response", err: ErrNoResponse, wantStatus: http.StatusOK},
		{name: "unauthorized", err: ErrUnauthorized, wantStatus: http.StatusUnauthorized},
		{name: "generic error", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(t, "/", "/", `{"name":"x"}`, withClaims(BC(
				func(*gin.Context, req, func() jwt.Claims) (Result, error) {
					return Result{Code: CodeErr}, tt.err
				})))
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// withClaims pre-populates claims so WC/BC get past their guard.
// withClaims 预先写入 claims，让 WC/BC 通过前置检查。
func withClaims(h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		SetClaims(c, func() jwt.Claims { return jwt.MapClaims{} })
		h(c)
	}
}

func TestHTTPStatusFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "status error", err: statusErr{http.StatusTeapot}, want: http.StatusTeapot},
		{name: "status out of range falls back", err: statusErr{42}, want: http.StatusInternalServerError},
		{name: "errorsx code", err: errorsx.InvalidParams("BAD"), want: http.StatusBadRequest},
		{name: "errorsx code out of range", err: errorsx.New(42, "WEIRD"), want: http.StatusInternalServerError},
		{name: "plain error", err: errors.New("boom"), want: http.StatusInternalServerError},
		{name: "wrapped status error", err: errors.Join(errors.New("ctx"), statusErr{http.StatusConflict}), want: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, httpStatusFromError(tt.err))
		})
	}
}
