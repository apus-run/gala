package errorsx

const (
	// 2xx success | 2xx 成功类
	StatusOK = "OK"

	// 4xx client errors | 4xx 客户端错误
	StatusBadRequest      = "BAD_REQUEST"
	StatusUnauthorized    = "UNAUTHORIZED"
	StatusForbidden       = "FORBIDDEN"
	StatusNotFound        = "NOT_FOUND"
	StatusConflict        = "CONFLICT"
	StatusPageNotFound    = "PAGE_NOT_FOUND"
	StatusTooManyRequests = "TOO_MANY_REQUESTS"
	StatusClientClosed    = "CLIENT_CLOSED"

	// Authentication related | 身份验证相关
	StatusTokenInvalid    = "TOKEN_INVALID"
	StatusTokenExpired    = "TOKEN_EXPIRED"
	StatusTokenInvalidSig = "TOKEN_INVALID_SIGNATURE"
	StatusUnauthenticated = "UNAUTHENTICATED"

	// Parameter validation | 参数校验
	StatusInvalidParams = "INVALID_PARAMS"
	StatusInvalidArgs   = "INVALID_ARGUMENTS"
	StatusBindError     = "BIND_ERROR"

	// 5xx server errors | 5xx 服务端错误
	StatusInternalServer     = "INTERNAL_SERVER"
	StatusServiceUnavailable = "SERVICE_UNAVAILABLE"
	StatusGatewayTimeout     = "GATEWAY_TIMEOUT"
	StatusPanicError         = "PANIC_ERROR"

	// Database related | 数据库相关
	StatusDBReadError        = "DB_READ_ERROR"
	StatusDBWriteError       = "DB_WRITE_ERROR"
	StatusDBTransactionError = "DB_TRANSACTION_ERROR"

	// Business logic | 业务逻辑
	StatusPermissionDenied = "PERMISSION_DENIED"
	StatusOperationFailed  = "OPERATION_FAILED"
)

var (
	// 2xx success | 2xx 成功类
	//
	// Deprecated: package-level *Error values are templates. Use Success(StatusOK)
	// or OK.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 Success(StatusOK) 或先 OK.Clone()。
	OK = Success(StatusOK)

	// 4xx client errors | 4xx 客户端错误
	//
	// Deprecated: package-level *Error values are templates. Use BadRequest(StatusBadRequest)
	// or ErrBadRequest.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 BadRequest(StatusBadRequest) 或先 ErrBadRequest.Clone()。
	ErrBadRequest = BadRequest(StatusBadRequest)
	// Deprecated: package-level *Error values are templates. Use Unauthorized(StatusUnauthorized)
	// or ErrUnauthorized.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 Unauthorized(StatusUnauthorized) 或先 ErrUnauthorized.Clone()。
	ErrUnauthorized = Unauthorized(StatusUnauthorized)
	// Deprecated: package-level *Error values are templates. Use Forbidden(StatusForbidden)
	// or ErrForbidden.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 Forbidden(StatusForbidden) 或先 ErrForbidden.Clone()。
	ErrForbidden = Forbidden(StatusForbidden)
	// Deprecated: package-level *Error values are templates. Use NotFound(StatusNotFound)
	// or ErrNotFound.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 NotFound(StatusNotFound) 或先 ErrNotFound.Clone()。
	ErrNotFound = NotFound(StatusNotFound)
	// Deprecated: package-level *Error values are templates. Use Conflict(StatusConflict)
	// or ErrConflict.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 Conflict(StatusConflict) 或先 ErrConflict.Clone()。
	ErrConflict = Conflict(StatusConflict)
	// Deprecated: package-level *Error values are templates. Use PageNotFound(StatusPageNotFound)
	// or ErrPageNotFound.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 PageNotFound(StatusPageNotFound) 或先 ErrPageNotFound.Clone()。
	ErrPageNotFound = PageNotFound(StatusPageNotFound)
	// Deprecated: package-level *Error values are templates. Use TooManyRequests(StatusTooManyRequests)
	// or ErrTooManyRequests.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 TooManyRequests(StatusTooManyRequests) 或先 ErrTooManyRequests.Clone()。
	ErrTooManyRequests = TooManyRequests(StatusTooManyRequests)
	// Deprecated: package-level *Error values are templates. Use ClientClosed(StatusClientClosed)
	// or ErrClientClosed.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 ClientClosed(StatusClientClosed) 或先 ErrClientClosed.Clone()。
	ErrClientClosed = ClientClosed(StatusClientClosed)

	// Authentication related | 身份验证相关
	//
	// Deprecated: package-level *Error values are templates. Use TokenInvalid(StatusTokenInvalid)
	// or ErrTokenInvalid.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 TokenInvalid(StatusTokenInvalid) 或先 ErrTokenInvalid.Clone()。
	ErrTokenInvalid = TokenInvalid(StatusTokenInvalid)
	// Deprecated: package-level *Error values are templates. Use TokenExpired(StatusTokenExpired)
	// or ErrTokenExpired.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 TokenExpired(StatusTokenExpired) 或先 ErrTokenExpired.Clone()。
	ErrTokenExpired = TokenExpired(StatusTokenExpired)
	// Deprecated: package-level *Error values are templates. Use TokenInvalidSignature(StatusTokenInvalidSig)
	// or ErrTokenInvalidSig.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 TokenInvalidSignature(StatusTokenInvalidSig) 或先 ErrTokenInvalidSig.Clone()。
	ErrTokenInvalidSig = TokenInvalidSignature(StatusTokenInvalidSig)
	// Deprecated: package-level *Error values are templates. Use Unauthenticated(StatusUnauthenticated)
	// or ErrUnauthenticated.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 Unauthenticated(StatusUnauthenticated) 或先 ErrUnauthenticated.Clone()。
	ErrUnauthenticated = Unauthenticated(StatusUnauthenticated)

	// Parameter validation | 参数校验
	//
	// Deprecated: package-level *Error values are templates. Use InvalidParams(StatusInvalidParams)
	// or ErrInvalidParams.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 InvalidParams(StatusInvalidParams) 或先 ErrInvalidParams.Clone()。
	ErrInvalidParams = InvalidParams(StatusInvalidParams)
	// Deprecated: package-level *Error values are templates. Use InvalidArguments(StatusInvalidArgs)
	// or ErrInvalidArgs.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 InvalidArguments(StatusInvalidArgs) 或先 ErrInvalidArgs.Clone()。
	ErrInvalidArgs = InvalidArguments(StatusInvalidArgs)
	// Deprecated: package-level *Error values are templates. Use BindError(StatusBindError)
	// or ErrBindError.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 BindError(StatusBindError) 或先 ErrBindError.Clone()。
	ErrBindError = BindError(StatusBindError)

	// 5xx server errors | 5xx 服务端错误
	//
	// Deprecated: package-level *Error values are templates. Use InternalServer(StatusInternalServer)
	// or ErrInternalServer.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 InternalServer(StatusInternalServer) 或先 ErrInternalServer.Clone()。
	ErrInternalServer = InternalServer(StatusInternalServer)
	// Deprecated: package-level *Error values are templates. Use ServiceUnavailable(StatusServiceUnavailable)
	// or ErrServiceUnavailable.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 ServiceUnavailable(StatusServiceUnavailable) 或先 ErrServiceUnavailable.Clone()。
	ErrServiceUnavailable = ServiceUnavailable(StatusServiceUnavailable)
	// Deprecated: package-level *Error values are templates. Use GatewayTimeout(StatusGatewayTimeout)
	// or ErrGatewayTimeout.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 GatewayTimeout(StatusGatewayTimeout) 或先 ErrGatewayTimeout.Clone()。
	ErrGatewayTimeout = GatewayTimeout(StatusGatewayTimeout)
	// Deprecated: package-level *Error values are templates. Use PanicError(StatusPanicError)
	// or ErrPanicError.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 PanicError(StatusPanicError) 或先 ErrPanicError.Clone()。
	ErrPanicError = PanicError(StatusPanicError)

	// Database related | 数据库相关
	//
	// Deprecated: package-level *Error values are templates. Use DBReadError(StatusDBReadError)
	// or ErrDBReadError.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 DBReadError(StatusDBReadError) 或先 ErrDBReadError.Clone()。
	ErrDBReadError = DBReadError(StatusDBReadError)
	// Deprecated: package-level *Error values are templates. Use DBWriteError(StatusDBWriteError)
	// or ErrDBWriteError.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 DBWriteError(StatusDBWriteError) 或先 ErrDBWriteError.Clone()。
	ErrDBWriteError = DBWriteError(StatusDBWriteError)
	// Deprecated: package-level *Error values are templates. Use DBTransactionError(StatusDBTransactionError)
	// or ErrDBTransactionError.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 DBTransactionError(StatusDBTransactionError) 或先 ErrDBTransactionError.Clone()。
	ErrDBTransactionError = DBTransactionError(StatusDBTransactionError)

	// Business logic | 业务逻辑
	//
	// Deprecated: package-level *Error values are templates. Use PermissionDenied(StatusPermissionDenied)
	// or ErrPermissionDenied.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 PermissionDenied(StatusPermissionDenied) 或先 ErrPermissionDenied.Clone()。
	ErrPermissionDenied = PermissionDenied(StatusPermissionDenied)
	// Deprecated: package-level *Error values are templates. Use OperationFailed(StatusOperationFailed)
	// or ErrOperationFailed.Clone() before setting message, details, cause, or stack.
	// Deprecated: 包级 *Error 是共享模板。设置 message、details、cause 或 stack
	// 之前，请改用 OperationFailed(StatusOperationFailed) 或先 ErrOperationFailed.Clone()。
	ErrOperationFailed = OperationFailed(StatusOperationFailed)
)
