package errorsx

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	httpstatus "github.com/apus-run/gala/pkg/errorsx/http"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

var (
	// EnableStackCapture 堆栈捕获开关（生产环境可关闭）
	EnableStackCapture = true
	// StackDepth 堆栈捕获深度
	StackDepth = 5
)

// Error 定义了项目体系中使用的错误类型，用于描述错误的详细信息
type Error struct {
	// 基本信息
	Code    int    `json:"code"`              // HTTP 状态码
	Status  string `json:"status"`            // 错误状态，业务错误码
	Message string `json:"message,omitempty"` // 错误消息
	Details any    `json:"details,omitempty"` // 详细信息，可包含 ErrorInfo、调试信息等

	// 额外信息
	cause error    // 原始错误信息，通常用于记录日志或调试
	stack []string // 错误发生时的调用栈信息，通常用于调试和排查问题
}

func New(code int, status string) *Error {
	return &Error{
		Code:   code,
		Status: status,
	}
}

// Error 实现 error 接口中的 `Error` 方法.
func (e *Error) Error() string {
	return fmt.Sprintf("error: code = %d, status = %s, message = %s", e.Code, e.Status, e.Message)
}

// WithDetails 用于设置与错误相关的详细信息，通常用于提供额外的上下文或调试信息
func (e *Error) WithDetails(details any) *Error {
	e.Details = details
	return e
}

// KV 使用 key-value 对设置详细信息
func (e *Error) KV(kvs ...string) *Error {
	if len(kvs)%2 != 0 {
		return e
	}
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	for i := 0; i < len(kvs); i += 2 {
		if i+1 < len(kvs) {
			e.Details.(map[string]string)[kvs[i]] = kvs[i+1]
		}
	}
	return e
}

// WithMessage 用于设置错误的详细信息，通常用于提供更具体的错误描述
func (e *Error) WithMessage(msg string) *Error {
	e.Message = msg
	return e
}

// WithCause with original error
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	if EnableStackCapture {
		e.stack = captureStack(2, StackDepth)
	}
	return e
}

// WithStack with stack
func (e *Error) WithStack() *Error {
	if EnableStackCapture {
		e.stack = captureStack(2, StackDepth)
	}
	return e
}

// WithRequestID 设置请求 ID
func (e *Error) WithRequestID(requestID string) *Error {
	return e.KV("X-Request-ID", requestID)
}

// WithUserID 设置用户 ID
func (e *Error) WithUserID(userID string) *Error {
	return e.KV("X-User-ID", userID)
}

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

// GRPCStatus 返回 gRPC 状态表示
func (e *Error) GRPCStatus() *status.Status {
	st := status.New(
		httpstatus.ToGRPCCode(e.Code),
		fmt.Sprintf("%s: %s", e.Status, e.Message),
	)

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

// Unwrap 返回原始错误
func (e *Error) Unwrap() error {
	return e.cause
}

// Is 判断当前错误是否与目标错误匹配.
// 它会比较 Error 实例的 Code 和 Status 字段.
// 如果 Code 和 Status 均相等，则返回 true；否则返回 false.
func (e *Error) Is(target error) bool {
	if targetErr := new(Error); errors.As(target, &targetErr) {
		return targetErr.Code == e.Code && targetErr.Status == e.Status
	}
	return errors.Is(e.cause, target)
}

func (e *Error) As(target any) bool {
	if t, ok := target.(**Error); ok {
		*t = e
		return true
	}
	return false
}

func (e *Error) Clone() *Error {
	return &Error{
		Code:    e.Code,
		Status:  e.Status,
		Message: e.Message,
		Details: e.Details,
		cause:   e.cause,
		stack:   e.stack,
	}
}

// Code 返回错误的 HTTP 代码
func Code(err error) int {
	if err == nil {
		return http.StatusOK //nolint:mnd
	}
	return FromError(err).Code
}

// Status 返回特定错误的状态
func Status(err error) string {
	if err == nil {
		return ""
	}
	return FromError(err).Status
}

// FromError 尝试将一个通用的 error 转换为自定义的 *Error 类型.
func FromError(err error) *Error {
	if err == nil {
		return nil
	}

	// 处理自定义错误类型
	var target *Error
	if errors.As(err, &target) {
		return target
	}

	// 处理 gRPC 错误
	if st, ok := status.FromError(err); ok {
		return fromGRPCStatus(st)
	}

	// 处理标准错误
	return &Error{
		Code:    http.StatusInternalServerError,
		Status:  "InternalError",
		Message: err.Error(),
		cause:   err,
		stack:   captureStack(2, 5),
	}
}

// 从 gRPC 状态转换
func fromGRPCStatus(st *status.Status) *Error {
	code := httpstatus.FromGRPCCode(st.Code())
	e := New(code, "GRPCError").WithMessage(st.Message())

	// 提取详情信息
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			e.Status = info.Reason
			e.Details = info.Metadata
		}
	}

	return e
}

// captureStack 增强的堆栈捕获方法
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
