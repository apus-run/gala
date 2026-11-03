package ginx

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/pkg/lang/value"
)

// WrapContext returns a context wrapped by this file
// WrapContext 返回被本文件包装后的 Context。
func WrapContext(c *gin.Context) *Context {
	return &Context{
		Context: c,
	}
}

// Handle convert HandlerFunc to gin.HandlerFunc
// Handle 把 HandlerFunc 转换为 gin.HandlerFunc。
func Handle(h HandlerFunc) gin.HandlerFunc {
	return func(ginCtx *gin.Context) {
		c := WrapContext(ginCtx)
		h(c)
	}
}

// ProxyHandle turns a ProxyHandlerFunc into a gin.HandlerFunc: on error or a
// nil proxy it aborts with 502, otherwise it forwards via the reverse proxy
// | ProxyHandle 把 ProxyHandlerFunc 转成 gin.HandlerFunc：出错或代理为空时
// 以 502 中止，否则交给反向代理转发。
func ProxyHandle(fn ProxyHandlerFunc) gin.HandlerFunc {
	return func(ginCtx *gin.Context) {
		c := WrapContext(ginCtx)
		proxy, err := fn(c)

		if err != nil || proxy == nil {
			// Internal errors are not echoed to the client, and the status
			// code must not be 200, otherwise monitoring cannot see failures.
			// 内部错误不回显给客户端，状态码也不能是 200，否则监控看不到失败。
			c.Data(http.StatusBadGateway, "text/plain; charset=utf-8", []byte(http.StatusText(http.StatusBadGateway)))
			c.Abort()
		} else {
			proxy.ServeHTTP(c.Writer, c.Request)
		}
	}
}

type ginKey struct{}

// NewGinContext returns a new Context that carries gin.Context value.
// NewGinContext 返回携带 gin.Context 值的新 context。
func NewGinContext(ctx context.Context, c *gin.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ginKey{}, c)
}

// FromGinContext returns the gin.Context value stored in ctx, if any.
// FromGinContext 取出 ctx 中保存的 gin.Context（如有）。
func FromGinContext(ctx context.Context) (c *gin.Context, ok bool) {
	if ctx == nil {
		return nil, false
	}
	c, ok = ctx.Value(ginKey{}).(*gin.Context)
	return
}

// Method returns the request HTTP method | Method 返回请求的 HTTP 方法.
func (ctx *Context) Method() string {
	return ctx.Request.Method
}

// GetContext returns the request's context | GetContext 返回请求自带的 context.
func (ctx *Context) GetContext() context.Context {
	return ctx.Context.Request.Context()
}

// URLPath returns the matched route path | URLPath 返回匹配的路由路径.
func (ctx *Context) URLPath() string {
	return ctx.FullPath()
}

// StatusCode returns the response status code | StatusCode 返回响应状态码.
func (ctx *Context) StatusCode() int {
	return ctx.Writer.Status()
}

// BytesWritten returns the number of bytes written | BytesWritten 返回已写出的字节数.
func (ctx *Context) BytesWritten() int64 {
	return int64(ctx.Writer.Size())
}

// Param returns a route parameter | Param 返回路由参数.
func (ctx *Context) Param(key string) value.AnyValue {
	return value.AnyValue{
		Value: ctx.Context.Param(key),
	}
}

// Query returns a URL query value | Query 返回 URL 查询参数.
func (ctx *Context) Query(key string) value.AnyValue {
	return value.AnyValue{
		Value: ctx.Context.Query(key),
	}
}

// Cookie returns a request cookie value | Cookie 返回请求 Cookie 的值.
func (ctx *Context) Cookie(key string) value.AnyValue {
	val, err := ctx.Context.Cookie(key)
	return value.AnyValue{
		Value: val,
		Error: err,
	}
}

// JSON returns JSON response
// e.x. {"code":<code>, "msg":<msg>, "data":<data>, "details":<details>}
// JSON 返回 JSON 响应，例如 {"code":<code>, "msg":<msg>, "data":<data>, "details":<details>}。
func (ctx *Context) JSON(httpStatus int, resp Result) {
	ctx.Context.JSON(httpStatus, resp)
}

// JSONOK returns JSON response with successful business code and data
// e.x. {"code": 200, "msg":"成功", "data":<data>}
// JSONOK 返回业务成功码与数据的 JSON 响应，例如 {"code": 200, "msg":"成功", "data":<data>}。
func (ctx *Context) JSONOK(msg string, data any) {
	j := new(Result)
	j.Code = CodeOK
	j.Msg = msg

	switch d := data.(type) {
	case error:
		j.Data = d.Error()
	case nil:
		j.Data = gin.H{}
	default:
		j.Data = data
	}

	ctx.Context.JSON(http.StatusOK, j)
}

// Success writes a JSON response with CodeOK and msg "ok", carrying the first
// data item if present | Success 写出 CodeOK、msg 为 "ok" 的 JSON 响应，
// 有参数时携带第一个 data。
func (ctx *Context) Success(data ...any) {
	j := new(Result)
	j.Code = CodeOK
	j.Msg = "ok"

	if len(data) > 0 {
		j.Data = data[0]
	} else {
		j.Data = ""
	}

	ctx.Context.JSON(http.StatusOK, j)
}

// JSONE returns JSON response with failure business code ,msg and data
// e.x. {"code":<code>, "msg":<msg>, "data":<data>}
// c.JSONE(5, "系统错误", err)
// JSONE 返回带失败业务码、msg 与 data 的 JSON 响应，例如 c.JSONE(5, "系统错误", err)。
func (ctx *Context) JSONE(code int, msg string, data any) {
	j := new(Result)
	j.Code = code
	j.Msg = msg

	switch d := data.(type) {
	case error:
		j.Data = d.Error()
	case nil:
		j.Data = gin.H{}
	default:
		j.Data = data
	}

	ctx.Context.JSON(http.StatusOK, j)
}

// NotFound responds 404 when no route matched | NotFound 在没有匹配路由时响应 404。
func (ctx *Context) NotFound() {
	ctx.String(http.StatusNotFound, "the route not found")
}

// GetClientLocale returns the client locale name
// GetClientLocale 返回客户端 locale 名称。
func (ctx *Context) GetClientLocale() string {
	v := ctx.GetHeader(AcceptLanguageHeaderName)

	return v
}

// GetClientTimezoneOffset returns the client timezone offset
// GetClientTimezoneOffset 返回客户端时区偏移。
func (ctx *Context) GetClientTimezoneOffset() (int16, error) {
	v := ctx.GetHeader(ClientTimezoneOffsetHeaderName)
	offset, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}

	return int16(offset), nil
}

// SetRequestId sets the given request id to context
// SetRequestId 把给定请求 id 写入 context。
func (ctx *Context) SetRequestId(requestId string) {
	ctx.Set(requestIdFieldKey, requestId)
}

// GetRequestId returns the current request id
// GetRequestId 返回当前请求 id，缺失或类型不符时返回空串。
func (ctx *Context) GetRequestId() string {
	requestId, exists := ctx.Get(requestIdFieldKey)

	if !exists {
		return ""
	}

	id, ok := requestId.(string)
	if !ok {
		return ""
	}
	return id
}
