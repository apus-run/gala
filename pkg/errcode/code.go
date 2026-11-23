package errcode

import "net/http"

// 通用错误定义公开语义。业务专用错误在各自的包中用 New 定义。
// 派生时使用 WithMessage、KV、Wrap，不要重新赋值这些共享变量。
var (
	// ErrInvalidArgument 表示请求格式或参数值不满足约束。
	ErrInvalidArgument = New(http.StatusBadRequest, "InvalidArgument", "Invalid argument.")

	// ErrUnauthenticated 表示缺少有效身份凭据。
	ErrUnauthenticated = New(http.StatusUnauthorized, "Unauthenticated", "Unauthenticated.")

	// ErrPermissionDenied 表示当前身份无权执行操作。
	ErrPermissionDenied = New(http.StatusForbidden, "PermissionDenied", "Permission denied.")

	// ErrNotFound 表示请求的资源不存在。
	ErrNotFound = New(http.StatusNotFound, "NotFound", "Resource not found.")

	// ErrMethodNotAllowed 表示资源不支持请求使用的 HTTP 方法。
	ErrMethodNotAllowed = New(http.StatusMethodNotAllowed, "MethodNotAllowed", "Method Not Allowed")

	// ErrConflict 表示操作与资源当前状态冲突。
	ErrConflict = New(http.StatusConflict, "Conflict", "Resource conflict.")

	// ErrTooManyRequests 表示请求频率或配额限制。
	ErrTooManyRequests = New(http.StatusTooManyRequests, "TooManyRequests", "Too many requests.")

	// ErrInternal 表示未分类的内部错误，对外使用通用消息。
	ErrInternal = New(http.StatusInternalServerError, "InternalError", "Internal server error.")

	// ErrTimeout 是 [Normalize] 对超时错误的默认分类，使用 HTTP 504。
	ErrTimeout = New(http.StatusGatewayTimeout, "DeadlineExceeded", "The operation timed out.")

	// ErrCanceled 是 [Normalize] 对 context.Canceled 的默认分类，使用非标准状态码 499。
	ErrCanceled = New(499, "Canceled", "Request canceled.")
)
