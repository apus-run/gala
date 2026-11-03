package luo

import "net/http"

// HandlerFunc is an optional error-returning HTTP handler. Use Adapt to register
// it with a Group or any net/http router. Existing route methods continue to
// accept standard http.HandlerFunc values.
// HandlerFunc 是可选的返回错误的 HTTP 处理器。经 Adapt 注册到 Group 或任意
// net/http 路由器；现有路由方法继续接受标准 http.HandlerFunc。
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// ErrorHandler handles a returned error. It owns logging, status mapping and
// response formatting; luo does not depend on a particular error type or codec.
// ErrorHandler 处理返回的错误，负责日志、状态码映射与响应格式化；
// luo 不依赖特定的错误类型或编解码器。
type ErrorHandler func(http.ResponseWriter, *http.Request, error)

// Adapt converts an error-returning handler to a standard http.HandlerFunc.
// Both arguments must be non-nil. The original writer and request are preserved,
// including streaming/hijacking interfaces. The handler should return errors
// before writing a response; otherwise onError must avoid writing it again.
// Adapt 把返回错误的处理器转换为标准 http.HandlerFunc。两个参数都必须非 nil。
// 原始 writer 与 request 保持不变（含流式/hijack 接口）。处理器应在写出响应前
// 返回错误，否则 onError 不得重复写响应。
func Adapt(handler HandlerFunc, onError ErrorHandler) http.HandlerFunc {
	if handler == nil || onError == nil {
		panic("luo: Adapt requires a handler and an error handler")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			onError(w, r, err)
		}
	}
}
