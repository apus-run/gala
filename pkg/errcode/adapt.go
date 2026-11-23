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
// 当前错误与 [ErrInternal] 同类时，Detail 使用预定义文案。Metadata 是可独立修改的副本。
func Respond(e *AppError) (int, Body) {
	status := e.HTTPStatus()
	if e == nil {
		return status, Body{}
	}
	detail := e.Message()
	if e.Is(ErrInternal) {
		detail = ErrInternal.Message()
	}
	return status, Body{
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Reason:   e.Reason(),
		Metadata: e.Metadata(),
	}
}

// AppHandler 将返回业务错误的函数适配为 [http.Handler]。
// 成功时由函数写响应并返回 nil；失败时在写响应前返回错误。
type AppHandler func(http.ResponseWriter, *http.Request) *AppError

// ServeHTTP 将返回的业务错误写入请求日志，并用 [Respond] 生成 problem+json 响应。HEAD 请求只发送响应头。
func (fn AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	appErr := fn(w, r)
	if appErr == nil {
		return
	}
	ctx := r.Context()
	slog.ErrorContext(ctx, "request failed", "err", appErr)
	status, body := Respond(appErr)
	header := w.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", ContentType)
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.ErrorContext(ctx, "write error response", "err", err)
	}
}
