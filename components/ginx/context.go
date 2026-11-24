package ginx

import (
	"context"
	"net/http"
	"net/http/httputil"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/sse"
	"github.com/apus-run/gala/pkg/lang/value"
)

// Context a wrapper of gin.Context | Context 是 gin.Context 的包装。
type Context struct {
	*gin.Context
}

// HandlerFunc defines the handler to wrap gin.Context
// HandlerFunc 定义包装 gin.Context 的处理函数。
type HandlerFunc func(*Context)

// ProxyHandlerFunc represents the reverse proxy handler function
// ProxyHandlerFunc 表示反向代理处理函数。
type ProxyHandlerFunc func(*Context) (*httputil.ReverseProxy, error)

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
	return ctx.GetHeader(AcceptLanguageHeaderName)
}

// GetClientTimezoneOffset returns the client timezone offset
// GetClientTimezoneOffset 返回客户端时区偏移。
func (ctx *Context) GetClientTimezoneOffset() (int16, error) {
	offset, err := strconv.Atoi(ctx.GetHeader(ClientTimezoneOffsetHeaderName))
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

// GetRequestId returns the current request id, or "" when absent or of the
// wrong type | GetRequestId 返回当前请求 id，缺失或类型不符时返回空串。
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

// EventStream opens a Server-Sent Events stream on this response and returns a
// writer for it. The header is committed immediately, so nothing may be written
// to the response before this call: on error the handler can still return a
// normal error response. The returned writer belongs to the request — do not use
// it after the handler returns.
//
// The stream is framework-neutral underneath: framing, flushing and write
// timeouts come from components/sse. Heartbeats, fan-out and reconnection are
// the caller's business, as they are for any SSE endpoint.
// EventStream 在本响应上开启 Server-Sent Events 流并返回其写入器。响应头会立刻提交，
// 因此调用前不能写出任何内容；出错时 handler 仍可返回普通错误响应。返回的写入器
// 属于本次请求，handler 返回后不得再使用。
//
// 底层与框架无关：分帧、刷新和写超时都来自 components/sse。心跳、广播与重连
// 由调用方负责——任何 SSE 端点都是如此。
//
// # Example | 示例
//
//	ginx.W(func(ctx *ginx.Context) (ginx.Result, error) {
//		sw, err := ctx.EventStream()
//		if err != nil {
//			return ginx.Result{}, err // nothing written yet, the wrapper renders it
//		}
//		for _, v := range values {
//			if err := sw.Send(sselib.Event{Data: v}); err != nil {
//				return ginx.Result{}, ginx.ErrNoResponse // client gone
//			}
//		}
//		return ginx.Result{}, ginx.ErrNoResponse // the stream is the response
//	})
func (ctx *Context) EventStream() (*sse.Writer, error) {
	return sse.NewWriter(ctx.Writer)
}
