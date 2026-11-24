package errcode

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ContentType 是 RFC 9457 为 JSON 形式的 problem details 定义的媒体类型。
const ContentType = "application/problem+json"

// Body 包含可公开的 Problem Details 字段。
type Body struct {
	Title    string            `json:"title,omitempty"`
	Status   int               `json:"status,omitempty"`
	Detail   string            `json:"detail,omitempty"`
	Reason   string            `json:"reason,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// HTTPStatus 返回用于写响应的状态码。nil 对应 200，
// 非 nil 错误的声明值在 400–599 范围内时原样返回，超出范围时返回 500。
func (e *AppError) HTTPStatus() int {
	if e == nil {
		return http.StatusOK
	}
	status := e.Code()
	if status < http.StatusBadRequest || status > 599 {
		return http.StatusInternalServerError
	}
	return status
}

// Respond 返回 [AppError.HTTPStatus] 和公开响应体。nil 输入返回 200 和 Body{}。
// 当前错误与 [ErrInternal] 同类时，Detail 使用预定义文案；零值错误回填 [ErrInternal] 的 Reason 和 Detail。
// Metadata 是可独立修改的副本。
func Respond(e *AppError) (int, Body) {
	status := e.HTTPStatus()
	if e == nil {
		return status, Body{}
	}
	reason, detail := e.Reason(), e.Message()
	if e.Is(ErrInternal) {
		detail = ErrInternal.Message()
	} else if reason == "" && detail == "" {
		// 零值 AppError：回填公开字段，客户端才能按 reason 分支。
		reason, detail = ErrInternal.Reason(), ErrInternal.Message()
	}
	return status, Body{
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Reason:   reason,
		Metadata: e.Metadata(),
	}
}

// AppHandler 将返回业务错误的函数适配为 [http.Handler]。
// 成功时由函数写响应并返回 nil；失败时在写响应前返回错误。
type AppHandler func(http.ResponseWriter, *http.Request) *AppError

// ServeHTTP 将返回的业务错误写入请求日志，并用 [Respond] 生成 problem+json 响应。HEAD 请求只发送响应头。
// 响应带 Cache-Control: no-store，避免中间缓存复用携带每请求数据的错误。
//
// 日志级别随响应状态区分：5xx 是服务端故障，记为 Error；其余是客户端可预期的失败，记为 Warn，
// 避免大量 404、400 淹没真正的故障。OpenTelemetry 的 HTTP 语义约定同样不把服务端 4xx 视为错误。
// 日志包含请求方法和路径，不含查询串，其中可能带有凭证。
//
// 响应写入失败只可能是连接已断开，没有可以报告的对象，与 [http.Error] 一样忽略。
func (fn AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	appErr := fn(w, r)
	if appErr == nil {
		return
	}
	status, body := Respond(appErr)
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.Log(r.Context(), level, "request failed", "method", r.Method, "path", r.URL.Path, "err", appErr)

	header := w.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", ContentType)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}
