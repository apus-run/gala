// Package errorsx provides a structured error type with HTTP code, business
// status, details, cause chain and stack capture | 结构化错误类型：HTTP 码、
// 业务状态、详情、错误链与堆栈捕获.
//
// # Notes | 说明
//
// Error implements Unwrap/Is/As (Is compares Code and Status); stack capture
// is governed by the package-level EnableStackCapture and StackDepth.
// FromError converts any error, including gRPC statuses, into *Error, and
// GRPCStatus converts back, attaching errdetails.ErrorInfo built from map
// details (AIP-193). The deprecated package-level Err* values are shared
// templates: Clone before mutating, or use the constructors, which pair with
// the Is* predicates.
// Error 实现了 Unwrap/Is/As（Is 比较 Code 与 Status）；堆栈捕获由包级
// EnableStackCapture 与 StackDepth 控制。FromError 能把任意错误（含 gRPC
// status）转为 *Error，GRPCStatus 反向转换并从 map 详情构造
// errdetails.ErrorInfo（AIP-193）。已弃用的包级 Err* 是共享模板：修改前先
// Clone，或改用配套 Is* 谓词的构造器。
//
// # Features | 功能
//
// - New plus WithMessage/WithDetails/KV/WithCause/WithStack builders | New 与 With* 链式构建
// - WithRequestID/WithUserID shorthand for key-value details | 请求/用户 ID 便捷写入详情
// - Category constructors and Is* predicates | 分类构造器与 Is* 谓词
// - Result[T], Must, Ignore helpers | Result[T] 与 Must、Ignore 辅助函数
//
// # Example | 示例
//
//	err := errorsx.BadRequest(errorsx.StatusBadRequest).
//		WithMessage("bad payload").WithCause(cause)
//	errorsx.Code(err)   // 400
//	errorsx.Status(err) // "BAD_REQUEST"
package errorsx

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"runtime"
	"strings"

	httpstatus "github.com/apus-run/gala/pkg/errorsx/http"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

var (
	// EnableStackCapture toggles stack capture; turn it off in production
	// EnableStackCapture 堆栈捕获开关（生产环境可关闭）.
	EnableStackCapture = true
	// StackDepth is the maximum captured stack depth | StackDepth 堆栈捕获深度.
	StackDepth = 5
)

// Error is the structured error type used across the project, carrying the
// full details of a failure | Error 是项目体系中使用的错误类型，用于描述错误的详细信息.
type Error struct {
	// Basic information | 基本信息
	Code    int    `json:"code"`              // HTTP status code | HTTP 状态码
	Status  string `json:"status"`            // business status code | 错误状态，业务错误码
	Message string `json:"message,omitempty"` // error message | 错误消息
	Details any    `json:"details,omitempty"` // extra details: ErrorInfo, debug info, ... | 详细信息，可包含 ErrorInfo、调试信息等

	// Extra information | 额外信息
	cause error    // original error, for logging or debugging | 原始错误信息，通常用于记录日志或调试
	stack []string // call stack at the time of the error | 错误发生时的调用栈信息，通常用于调试和排查问题
}

// New creates an *Error with the given HTTP code and business status
// New 按给定 HTTP 码与业务状态创建 *Error.
func New(code int, status string) *Error {
	return &Error{
		Code:   code,
		Status: status,
	}
}

// Error implements the error interface | Error 实现 error 接口中的 Error 方法.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause != nil {
		return fmt.Sprintf("error: code = %d, status = %s, message = %s, cause = %v", e.Code, e.Status, e.Message, e.cause)
	}
	return fmt.Sprintf("error: code = %d, status = %s, message = %s", e.Code, e.Status, e.Message)
}

// Stack returns the call stack captured when the error occurred
// Stack 返回错误发生时的调用栈信息.
func (e *Error) Stack() []string {
	return e.stack
}

// HTTPStatus returns the HTTP status code represented by this error
// HTTPStatus 返回该错误对应的 HTTP 状态码.
func (e *Error) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	return e.Code
}

// WithDetails sets the detailed information attached to the error, usually
// extra context for debugging | WithDetails 设置与错误相关的详细信息，
// 通常用于提供额外的上下文或调试信息.
func (e *Error) WithDetails(details any) *Error {
	e.Details = details
	return e
}

// KV sets detail entries from key-value pairs.
//
// It mutates e in place: the package-level Err* templates are shared, so
// Clone first before calling, otherwise concurrent calls trigger concurrent
// map writes and Details written by one request leak into others. If Details
// was already set to another type by WithDetails it is left untouched and e
// is returned as is.
// KV 使用 key-value 对设置详细信息。
//
// 原地修改 e：包级的 Err* 模板是共享的，必须先 Clone 再调用，否则并发调用会
// 触发 concurrent map writes，并且一个请求写入的 Details 会出现在其他请求里。
// Details 已被 WithDetails 设置成其他类型时，不覆盖它，原样返回。
func (e *Error) KV(kvs ...string) *Error {
	if len(kvs)%2 != 0 {
		return e
	}
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	details, ok := e.Details.(map[string]string)
	if !ok {
		return e
	}
	for i := 0; i+1 < len(kvs); i += 2 {
		details[kvs[i]] = kvs[i+1]
	}
	return e
}

// WithMessage sets a more specific error description
// WithMessage 设置错误的详细信息，通常用于提供更具体的错误描述.
func (e *Error) WithMessage(msg string) *Error {
	e.Message = msg
	return e
}

// WithCause attaches the original error | WithCause 挂载原始错误.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	if EnableStackCapture {
		e.stack = captureStack(2, StackDepth)
	}
	return e
}

// WithStack captures the call stack | WithStack 捕获调用栈.
func (e *Error) WithStack() *Error {
	if EnableStackCapture {
		e.stack = captureStack(2, StackDepth)
	}
	return e
}

// WithRequestID records the request ID as a detail entry
// WithRequestID 设置请求 ID.
func (e *Error) WithRequestID(requestID string) *Error {
	return e.KV("X-Request-ID", requestID)
}

// WithUserID records the user ID as a detail entry | WithUserID 设置用户 ID.
func (e *Error) WithUserID(userID string) *Error {
	return e.KV("X-User-ID", userID)
}

// Format implements fmt.Formatter: %v renders the full error, other verbs
// render only the message | Format 实现 fmt.Formatter：%v 输出完整错误，
// 其余动词只输出 message.
func (e *Error) Format(state fmt.State, verb rune) {
	switch verb {
	case 'v':
		str := bytes.NewBuffer([]byte{})
		str.WriteString(fmt.Sprintf("code: %d, ", e.Code))
		str.WriteString("status: ")
		str.WriteString(e.Status + ", ")
		str.WriteString("message: ")
		str.WriteString(e.Message)
		if e.Details != nil {
			str.WriteString(", details: ")
			fmt.Fprint(str, e.Details)
		}
		if e.cause != nil {
			str.WriteString(", cause: ")
			str.WriteString(e.cause.Error())
		}
		if len(e.stack) > 0 {
			str.WriteString(", stack: ")
			for _, s := range e.stack {
				str.WriteString(fmt.Sprintf("%s\n", s))
			}
		}
		fmt.Fprintf(state, "%s", strings.Trim(str.String(), "\r\n\t"))
	default:
		fmt.Fprintf(state, "%s", e.Message)
	}
}

// GRPCStatus returns the gRPC status representation | GRPCStatus 返回 gRPC 状态表示.
func (e *Error) GRPCStatus() *status.Status {
	st := status.New(
		httpstatus.ToGRPCCode(e.Code),
		fmt.Sprintf("%s: %s", e.Status, e.Message),
	)

	// attach ErrorInfo details (per the AIP-193 standard)
	// 添加 ErrorInfo 详情（符合 AIP-193 标准要求）
	if e.Details != nil {
		if metadata, ok := e.Details.(map[string]string); ok {
			st, _ = st.WithDetails(&errdetails.ErrorInfo{
				Reason:   e.Status,
				Metadata: metadata,
			})
		}
	}

	return st
}

// Unwrap returns the original error so errors.Is and errors.As can walk and
// match the chain | Unwrap 返回原始错误，便于使用 errors.Is 和 errors.As 进行错误链的遍历和匹配.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is reports whether the target error matches this one. It compares the Code
// and Status fields: true only when both are equal, false otherwise.
// Is 判断当前错误是否与目标错误匹配。
// 它会比较 Error 实例的 Code 和 Status 字段：
// 如果 Code 和 Status 均相等，则返回 true；否则返回 false。
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	if targetErr, ok := errors.AsType[*Error](target); ok {
		return targetErr != nil && targetErr.Code == e.Code && targetErr.Status == e.Status
	}
	return errors.Is(e.cause, target)
}

// As lets errors.As extract the *Error itself | As 让 errors.As 能提取出 *Error 本身.
func (e *Error) As(target any) bool {
	if t, ok := target.(**Error); ok {
		*t = e
		return true
	}
	return false
}

// Clone returns a deep copy, safe to mutate | Clone 返回可安全修改的深拷贝.
func (e *Error) Clone() *Error {
	if e == nil {
		return nil
	}

	copied := *e
	copied.Details = cloneDetails(e.Details)
	copied.stack = append([]string(nil), e.stack...)
	return &copied
}

func cloneDetails(details any) any {
	switch d := details.(type) {
	case map[string]string:
		cloned := make(map[string]string, len(d))
		maps.Copy(cloned, d)
		return cloned
	case map[string]any:
		cloned := make(map[string]any, len(d))
		maps.Copy(cloned, d)
		return cloned
	case []string:
		return append([]string(nil), d...)
	default:
		return details
	}
}

// Code returns the HTTP code of the error | Code 返回错误的 HTTP 代码.
func Code(err error) int {
	if err == nil {
		return http.StatusOK //nolint:mnd
	}
	return FromError(err).Code
}

// Status returns the business status of the error | Status 返回特定错误的状态.
func Status(err error) string {
	if err == nil {
		return ""
	}
	return FromError(err).Status
}

// FromError converts a generic error into the custom *Error type
// FromError 尝试将一个通用的 error 转换为自定义的 *Error 类型.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}

	// handle the custom error type. When the interface holds (*Error)(nil),
	// AsType still returns ok with a nil target; returning it directly would
	// make Code/Status dereference nil
	// 处理自定义错误类型。接口里装着 (*Error)(nil) 时 AsType 也会返回 ok，
	// 此时 target 为 nil，直接返回会让 Code/Status 解引用 nil
	if target, ok := errors.AsType[*Error](err); ok {
		if target == nil {
			return &Error{Code: http.StatusInternalServerError, Status: "InternalError", Message: "nil error"}
		}
		return target
	}

	// handle gRPC errors
	// 处理 gRPC 错误
	if st, ok := status.FromError(err); ok {
		return fromGRPCStatus(st)
	}

	// handle standard errors
	// 处理标准错误
	return &Error{
		Code:    http.StatusInternalServerError,
		Status:  "InternalError",
		Message: err.Error(),
		cause:   err,
		stack:   captureStack(2, 5),
	}
}

// fromGRPCStatus converts a gRPC status | fromGRPCStatus 从 gRPC 状态转换.
func fromGRPCStatus(st *status.Status) *Error {
	code := httpstatus.FromGRPCCode(st.Code())
	e := New(code, "GRPCError").WithMessage(st.Message())

	// extract detail entries
	// 提取详情信息
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			e.Status = info.Reason
			e.Details = info.Metadata
		}
	}

	return e
}

// captureStack captures the call stack.
// skip: stack levels to skip (usually 2: this call and runtime.Callers)
// depth: maximum depth to capture (0 means unlimited)
// captureStack 增强的堆栈捕获方法。
// skip: 跳过的调用层级（通常为 2：跳过自身和 runtime.Callers）
// depth: 最大捕获深度（0 表示无限制）
func captureStack(skip, depth int) []string {
	if depth == 0 {
		return nil
	}
	pcs := make([]uintptr, depth)
	n := runtime.Callers(skip+1, pcs)
	if n == 0 {
		return nil
	}

	frames := runtime.CallersFrames(pcs[:n])
	stack := make([]string, 0, n)
	for {
		frame, more := frames.Next()
		stack = append(stack,
			fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function))
		if !more {
			break
		}
	}
	return stack
}
