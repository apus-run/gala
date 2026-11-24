// Package errcode 定义 Go HTTP 服务的业务错误及其公开响应。
//
// 业务包用 [New] 定义错误，用 [errors.Is] 判断错误类别，用 [errors.AsType] 从错误链中提取已有的 *AppError。
// 在 HTTP 出口，用 [Normalize] 为收到的 error 确定分类，再用 [Respond] 生成公开响应。
package errcode

import (
	"fmt"
	"log/slog"
	"maps"
)

// AppError 用 HTTP 状态码和稳定的 Reason 标识业务错误。消息和元数据可以公开，底层原因供日志和错误链检查使用。
//
// 预定义错误可跨请求共享。WithMessage、KV 和 Wrap 均返回副本，Metadata 返回可独立修改的 map。
// 调用方应避免覆写共享实例或重新赋值预定义变量。底层原因的并发安全由其实现负责。
//
// 成功时返回 nil。AppError 的零值按内部服务错误（HTTP 500）响应。
//
// Error、Unwrap、Is 和 LogValue 接受 nil 接收者，因为标准库遍历错误链时会调用它们。
// 其余方法与普通指针方法一样，在 nil 接收者上会 panic。[Normalize] 对 nil 输入返回 nil，
// 链式调用 Normalize(err).KV(...) 前需先确认 err 非 nil。
type AppError struct {
	code     int               // HTTP 状态码；在响应边界校验。
	reason   string            // 稳定的机器可读标识，如 "NotFound.User"。
	message  string            // 可公开的消息，不用于判断错误类别。
	metadata map[string]string // 可公开的补充信息；只通过复制后写入修改。
	cause    error             // 原始原因，不进入 Body。
}

// New 用声明的 HTTP 状态码、稳定的业务标识和公开文案创建错误。
// 随请求变化的公开信息可通过 [AppError.KV] 添加。写响应时由 [AppError.HTTPStatus] 校验状态码。
func New(status int, reason, message string) *AppError {
	return &AppError{code: status, reason: reason, message: message}
}

// Code 返回声明时的 HTTP 状态码。写响应用 [AppError.HTTPStatus]。
func (e *AppError) Code() int { return e.code }

// Reason 返回稳定的机器可读标识。
func (e *AppError) Reason() string { return e.reason }

// Message 返回调用方设置的文案。[Respond] 为 [ErrInternal] 使用预定义文案。
func (e *AppError) Message() string { return e.message }

// Metadata 返回元数据的副本；没有元数据时返回 nil。
// 修改返回的 map 不会影响原错误。
func (e *AppError) Metadata() map[string]string { return maps.Clone(e.metadata) }

// Error 返回服务端诊断文本，包含已设置的元数据和底层原因。nil 接收者返回 "<nil>"。
// 生成客户端响应使用 [Respond]。
func (e *AppError) Error() string {
	if e == nil {
		return "<nil>"
	}
	s := fmt.Sprintf("code=%d reason=%s message=%s", e.code, e.reason, e.message)
	if len(e.metadata) > 0 {
		s += fmt.Sprintf(" metadata=%v", e.metadata)
	}
	if e.cause != nil {
		s += fmt.Sprintf(" cause=%v", e.cause)
	}
	return s
}

// LogValue 实现 [slog.LogValuer]，输出供服务端诊断的结构化字段。
// 有底层原因时记录其文本。
func (e *AppError) LogValue() slog.Value {
	if e == nil {
		return slog.StringValue(e.Error())
	}
	attrs := []slog.Attr{
		slog.Int("code", e.code),
		slog.String("reason", e.reason),
		slog.String("message", e.message),
	}
	if len(e.metadata) > 0 {
		attrs = append(attrs, slog.Any("metadata", e.Metadata()))
	}
	if e.cause != nil {
		attrs = append(attrs, slog.String("cause", fmt.Sprint(e.cause)))
	}
	return slog.GroupValue(attrs...)
}

// Unwrap 返回底层原因，供标准库 errors.Is 和 errors.As 遍历。
// nil 接收者或没有底层原因时返回 nil。
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is 比较当前错误与 target 的声明状态码和 Reason。任一方为 nil 时返回 false。
// 在整条错误链中匹配业务错误时使用 [errors.Is]，例如 errors.Is(err, ErrNotFound)。
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	return e != nil && ok && t != nil && e.code == t.code && e.reason == t.reason
}

// WithMessage 返回替换了公开消息的副本，保留其余字段和底层原因。
func (e *AppError) WithMessage(message string) *AppError {
	c := *e
	c.message = message
	return &c
}

// KV 返回新增或替换一对公开元数据的副本。
// 同名键以本次值为准；需要多对元数据时可以链式调用。
func (e *AppError) KV(key, value string) *AppError {
	m := make(map[string]string, len(e.metadata)+1)
	maps.Copy(m, e.metadata)
	m[key] = value
	c := *e
	c.metadata = m
	return &c
}

// Wrap 返回将底层原因替换为 cause 的副本，保留公开字段。
// cause 为 nil 时清除已有原因。
func (e *AppError) Wrap(cause error) *AppError {
	c := *e
	c.cause = cause
	return &c
}
