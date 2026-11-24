package ginx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/sse"
)

// callContext runs handler against a real gin engine so the writer keeps gin's
// wrapper (which supports flushing) and route parameters are populated.
// callContext 在真实 gin 引擎上运行 handler，保留 gin 的 writer 包装（支持 Flush），
// 并填充路由参数。
func callContext(t *testing.T, pattern, target string, handler func(*Context)) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var captured *gin.Context
	engine := gin.New()
	engine.GET(pattern, func(c *gin.Context) {
		captured = c
		handler(&Context{Context: c})
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "abc"})
	engine.ServeHTTP(w, r)
	return w, captured
}

func TestContextEventStream(t *testing.T) {
	w, _ := callContext(t, "/stream", "/stream", func(ctx *Context) {
		sw, err := ctx.EventStream()
		require.NoError(t, err)
		require.NoError(t, sw.Send(sse.Event{ID: "1", Data: []byte("hello")}))
		require.NoError(t, sw.Send(sse.Event{Data: []byte("world")}))
	})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	body := w.Body.String()
	assert.Contains(t, body, "id: 1\n")
	assert.Contains(t, body, "data: hello\n")
	assert.Contains(t, body, "data: world\n")
	assert.True(t, strings.HasSuffix(body, "\n\n"), "each event ends with a blank line")
}

func TestContextAccessors(t *testing.T) {
	w, ginCtx := callContext(t, "/rooms/:room", "/rooms/lobby?count=7", func(ctx *Context) {
		assert.Equal(t, "lobby", ctx.Param("room").StringOrDefault("none"))
		assert.Equal(t, "7", ctx.Query("count").StringOrDefault("0"))
		assert.Equal(t, "abc", ctx.Cookie("session").StringOrDefault(""))

		// The relaxed accessor parses query strings; the strict one does not.
		// 宽松访问器解析字符串；严格访问器不解析。
		n, err := ctx.Query("count").AsInt()
		require.NoError(t, err)
		assert.Equal(t, 7, n)
		assert.Equal(t, 0, ctx.Query("count").IntOrDefault(0), "Int is a strict assertion over string values")

		// Missing input is reported as an error by Cookie, and as a default elsewhere.
		// 缺失时 Cookie 通过 error 表示，其余访问器走默认值。
		assert.Error(t, ctx.Cookie("absent").Error)
		assert.Equal(t, "", ctx.Param("absent").StringOrDefault(""))
	})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, ginCtx)
}

func TestContextRequestMetadata(t *testing.T) {
	callContext(t, "/rooms/:room", "/rooms/lobby?x=1", func(ctx *Context) {
		assert.Equal(t, http.MethodGet, ctx.Method())
		assert.Equal(t, "/rooms/:room", ctx.URLPath())
		assert.Equal(t, http.StatusOK, ctx.StatusCode())
		assert.NotNil(t, ctx.GetContext())

		// Request id round-trip, including the missing and wrong-type cases.
		// request id 往返，含缺失与类型不符。
		assert.Equal(t, "", ctx.GetRequestId())
		ctx.SetRequestId("req-1")
		assert.Equal(t, "req-1", ctx.GetRequestId())
		ctx.Set(requestIdFieldKey, 42)
		assert.Equal(t, "", ctx.GetRequestId(), "a non-string id must not panic")

		ctx.SetRequestId("req-2")
		ctx.Success(ctx.GetRequestId())
		assert.Equal(t, http.StatusOK, ctx.StatusCode())
		assert.Positive(t, ctx.BytesWritten(), "the response has been written")
	})
}

func TestContextCookieErrorPropagates(t *testing.T) {
	callContext(t, "/c", "/c", func(ctx *Context) {
		av := ctx.Cookie("absent")
		assert.Error(t, av.Error)
		// A held error short-circuits every accessor.
		// 持有错误时所有取值方法直接返回该错误。
		_, err := av.String()
		assert.ErrorIs(t, err, av.Error)
	})
}

func TestContextResponses(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		w, _ := callContext(t, "/", "/", func(ctx *Context) {
			ctx.JSON(http.StatusCreated, Result{Code: CodeErr, Msg: "bad"})
		})
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Contains(t, w.Body.String(), `"msg":"bad"`)
	})

	t.Run("JSONOK with data", func(t *testing.T) {
		w, _ := callContext(t, "/", "/", func(ctx *Context) { ctx.JSONOK("ok", gin.H{"id": 1}) })
		assert.Contains(t, w.Body.String(), `"code":0`)
		assert.Contains(t, w.Body.String(), `"id":1`)
	})

	t.Run("JSONOK with error and nil", func(t *testing.T) {
		w, _ := callContext(t, "/", "/", func(ctx *Context) {
			ctx.JSONOK("failed", assert.AnError)
		})
		assert.Contains(t, w.Body.String(), assert.AnError.Error())

		w, _ = callContext(t, "/", "/", func(ctx *Context) { ctx.JSONOK("none", nil) })
		assert.Contains(t, w.Body.String(), `"data":{}`)
	})

	t.Run("Success", func(t *testing.T) {
		w, _ := callContext(t, "/", "/", func(ctx *Context) { ctx.Success("payload") })
		assert.Contains(t, w.Body.String(), `"msg":"ok"`)
		assert.Contains(t, w.Body.String(), `"data":"payload"`)

		w, _ = callContext(t, "/", "/", func(ctx *Context) { ctx.Success() })
		assert.Contains(t, w.Body.String(), `"data":""`)
	})

	t.Run("JSONE", func(t *testing.T) {
		w, _ := callContext(t, "/", "/", func(ctx *Context) { ctx.JSONE(7, "boom", assert.AnError) })
		assert.Contains(t, w.Body.String(), `"code":7`)
		assert.Contains(t, w.Body.String(), assert.AnError.Error())

		w, _ = callContext(t, "/", "/", func(ctx *Context) { ctx.JSONE(8, "nil", nil) })
		assert.Contains(t, w.Body.String(), `"data":{}`)

		// Non-error, non-nil data passes through unchanged.
		// 非 error 且非 nil 的 data 原样写出。
		w, _ = callContext(t, "/", "/", func(ctx *Context) { ctx.JSONE(9, "data", gin.H{"id": 2}) })
		assert.Contains(t, w.Body.String(), `"data":{"id":2}`)
	})

	t.Run("NotFound", func(t *testing.T) {
		w, _ := callContext(t, "/", "/", func(ctx *Context) { ctx.NotFound() })
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "the route not found")
	})
}

func TestContextHeaderHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var locale string
	var offset int16
	var offsetErr error
	engine := gin.New()
	engine.GET("/", func(c *gin.Context) {
		ctx := &Context{Context: c}
		locale = ctx.GetClientLocale()
		offset, offsetErr = ctx.GetClientTimezoneOffset()
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(AcceptLanguageHeaderName, "zh-CN")
	r.Header.Set(ClientTimezoneOffsetHeaderName, "480")
	engine.ServeHTTP(httptest.NewRecorder(), r)

	assert.Equal(t, "zh-CN", locale)
	assert.Equal(t, int16(480), offset)
	assert.NoError(t, offsetErr)

	// A missing or malformed header surfaces as an error, not a silent zero.
	// 缺失或非法的头返回错误，而不是静默的零值。
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Error(t, offsetErr)
	assert.Zero(t, offset)
}

func TestWrapContextAndHandle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var method, path string
	var size int64
	engine := gin.New()
	engine.GET("/", Handle(func(ctx *Context) {
		method, path = ctx.Method(), ctx.URLPath()
		ctx.String(http.StatusTeapot, "hello")
		// gin's writer starts at -1 and reports the real size after a write.
		// gin 的 writer 初始为 -1，写入后才报告真实大小。
		size = ctx.BytesWritten()
	}))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.Equal(t, "hello", w.Body.String())
	assert.Equal(t, http.MethodGet, method)
	assert.Equal(t, "/", path)
	assert.Equal(t, int64(len("hello")), size)
}
