package errorsx

const (
	// 2xx
	StatusOK = "OK"

	// 4xx
	StatusBadRequest      = "BAD_REQUEST"
	StatusUnauthorized    = "UNAUTHORIZED"
	StatusForbidden       = "FORBIDDEN"
	StatusNotFound        = "NOT_FOUND"
	StatusConflict        = "CONFLICT"
	StatusPageNotFound    = "PAGE_NOT_FOUND"
	StatusTooManyRequests = "TOO_MANY_REQUESTS"
	StatusClientClosed    = "CLIENT_CLOSED"

	// 身份验证相关
	StatusTokenInvalid    = "TOKEN_INVALID"
	StatusTokenExpired    = "TOKEN_EXPIRED"
	StatusTokenInvalidSig = "TOKEN_INVALID_SIGNATURE"
	StatusUnauthenticated = "UNAUTHENTICATED"

	// 参数校验
	StatusInvalidParams = "INVALID_PARAMS"
	StatusInvalidArgs   = "INVALID_ARGUMENTS"
	StatusBindError     = "BIND_ERROR"

	// 5xx
	StatusInternalServer     = "INTERNAL_SERVER"
	StatusServiceUnavailable = "SERVICE_UNAVAILABLE"
	StatusGatewayTimeout     = "GATEWAY_TIMEOUT"
	StatusPanicError         = "PANIC_ERROR"

	// 数据库相关
	StatusDBReadError        = "DB_READ_ERROR"
	StatusDBWriteError       = "DB_WRITE_ERROR"
	StatusDBTransactionError = "DB_TRANSACTION_ERROR"

	// 业务逻辑
	StatusPermissionDenied = "PERMISSION_DENIED"
	StatusOperationFailed  = "OPERATION_FAILED"
)

var (
	// 2xx
	// Deprecated: package-level *Error values are templates. Use Success(StatusOK)
	// or OK.Clone() before setting message, details, cause, or stack.
	OK = Success(StatusOK)

	// 4xx
	// Deprecated: package-level *Error values are templates. Use BadRequest(StatusBadRequest)
	// or ErrBadRequest.Clone() before setting message, details, cause, or stack.
	ErrBadRequest = BadRequest(StatusBadRequest)
	// Deprecated: package-level *Error values are templates. Use Unauthorized(StatusUnauthorized)
	// or ErrUnauthorized.Clone() before setting message, details, cause, or stack.
	ErrUnauthorized = Unauthorized(StatusUnauthorized)
	// Deprecated: package-level *Error values are templates. Use Forbidden(StatusForbidden)
	// or ErrForbidden.Clone() before setting message, details, cause, or stack.
	ErrForbidden = Forbidden(StatusForbidden)
	// Deprecated: package-level *Error values are templates. Use NotFound(StatusNotFound)
	// or ErrNotFound.Clone() before setting message, details, cause, or stack.
	ErrNotFound = NotFound(StatusNotFound)
	// Deprecated: package-level *Error values are templates. Use Conflict(StatusConflict)
	// or ErrConflict.Clone() before setting message, details, cause, or stack.
	ErrConflict = Conflict(StatusConflict)
	// Deprecated: package-level *Error values are templates. Use PageNotFound(StatusPageNotFound)
	// or ErrPageNotFound.Clone() before setting message, details, cause, or stack.
	ErrPageNotFound = PageNotFound(StatusPageNotFound)
	// Deprecated: package-level *Error values are templates. Use TooManyRequests(StatusTooManyRequests)
	// or ErrTooManyRequests.Clone() before setting message, details, cause, or stack.
	ErrTooManyRequests = TooManyRequests(StatusTooManyRequests)
	// Deprecated: package-level *Error values are templates. Use ClientClosed(StatusClientClosed)
	// or ErrClientClosed.Clone() before setting message, details, cause, or stack.
	ErrClientClosed = ClientClosed(StatusClientClosed)

	// 身份验证相关
	// Deprecated: package-level *Error values are templates. Use TokenInvalid(StatusTokenInvalid)
	// or ErrTokenInvalid.Clone() before setting message, details, cause, or stack.
	ErrTokenInvalid = TokenInvalid(StatusTokenInvalid)
	// Deprecated: package-level *Error values are templates. Use TokenExpired(StatusTokenExpired)
	// or ErrTokenExpired.Clone() before setting message, details, cause, or stack.
	ErrTokenExpired = TokenExpired(StatusTokenExpired)
	// Deprecated: package-level *Error values are templates. Use TokenInvalidSignature(StatusTokenInvalidSig)
	// or ErrTokenInvalidSig.Clone() before setting message, details, cause, or stack.
	ErrTokenInvalidSig = TokenInvalidSignature(StatusTokenInvalidSig)
	// Deprecated: package-level *Error values are templates. Use Unauthenticated(StatusUnauthenticated)
	// or ErrUnauthenticated.Clone() before setting message, details, cause, or stack.
	ErrUnauthenticated = Unauthenticated(StatusUnauthenticated)

	// 参数校验
	// Deprecated: package-level *Error values are templates. Use InvalidParams(StatusInvalidParams)
	// or ErrInvalidParams.Clone() before setting message, details, cause, or stack.
	ErrInvalidParams = InvalidParams(StatusInvalidParams)
	// Deprecated: package-level *Error values are templates. Use InvalidArguments(StatusInvalidArgs)
	// or ErrInvalidArgs.Clone() before setting message, details, cause, or stack.
	ErrInvalidArgs = InvalidArguments(StatusInvalidArgs)
	// Deprecated: package-level *Error values are templates. Use BindError(StatusBindError)
	// or ErrBindError.Clone() before setting message, details, cause, or stack.
	ErrBindError = BindError(StatusBindError)

	// 5xx
	// Deprecated: package-level *Error values are templates. Use InternalServer(StatusInternalServer)
	// or ErrInternalServer.Clone() before setting message, details, cause, or stack.
	ErrInternalServer = InternalServer(StatusInternalServer)
	// Deprecated: package-level *Error values are templates. Use ServiceUnavailable(StatusServiceUnavailable)
	// or ErrServiceUnavailable.Clone() before setting message, details, cause, or stack.
	ErrServiceUnavailable = ServiceUnavailable(StatusServiceUnavailable)
	// Deprecated: package-level *Error values are templates. Use GatewayTimeout(StatusGatewayTimeout)
	// or ErrGatewayTimeout.Clone() before setting message, details, cause, or stack.
	ErrGatewayTimeout = GatewayTimeout(StatusGatewayTimeout)
	// Deprecated: package-level *Error values are templates. Use PanicError(StatusPanicError)
	// or ErrPanicError.Clone() before setting message, details, cause, or stack.
	ErrPanicError = PanicError(StatusPanicError)

	// 数据库相关
	// Deprecated: package-level *Error values are templates. Use DBReadError(StatusDBReadError)
	// or ErrDBReadError.Clone() before setting message, details, cause, or stack.
	ErrDBReadError = DBReadError(StatusDBReadError)
	// Deprecated: package-level *Error values are templates. Use DBWriteError(StatusDBWriteError)
	// or ErrDBWriteError.Clone() before setting message, details, cause, or stack.
	ErrDBWriteError = DBWriteError(StatusDBWriteError)
	// Deprecated: package-level *Error values are templates. Use DBTransactionError(StatusDBTransactionError)
	// or ErrDBTransactionError.Clone() before setting message, details, cause, or stack.
	ErrDBTransactionError = DBTransactionError(StatusDBTransactionError)

	// 业务逻辑
	// Deprecated: package-level *Error values are templates. Use PermissionDenied(StatusPermissionDenied)
	// or ErrPermissionDenied.Clone() before setting message, details, cause, or stack.
	ErrPermissionDenied = PermissionDenied(StatusPermissionDenied)
	// Deprecated: package-level *Error values are templates. Use OperationFailed(StatusOperationFailed)
	// or ErrOperationFailed.Clone() before setting message, details, cause, or stack.
	ErrOperationFailed = OperationFailed(StatusOperationFailed)
)
