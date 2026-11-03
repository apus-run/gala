package errorsx

import "net/http"

// Success creates a new Success error | Success 创建一个新的 Success 错误.
func Success(status string) *Error {
	return New(http.StatusOK, status).WithMessage("OK")
}

// IsSuccess determines if err is the Success error | IsSuccess 判断 err 是否是 Success 错误.
func IsSuccess(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusOK &&
		err.Message == "OK"
}

// BadRequest creates a new BadRequest error | BadRequest 创建一个新的 BadRequest 错误.
func BadRequest(status string) *Error {
	return New(http.StatusBadRequest, status).WithMessage("Bad Request")
}

// IsBadRequest determines if err is BadRequest error | IsBadRequest 判断 err 是否是 BadRequest 错误.
func IsBadRequest(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusBadRequest &&
		err.Message == "Bad Request"
}

// Unauthorized creates a new Unauthorized error | Unauthorized 创建一个新的 Unauthorized 错误.
func Unauthorized(status string) *Error {
	return New(http.StatusUnauthorized, status).WithMessage("Unauthorized")
}

// IsUnauthorized determines if err is Unauthorized error | IsUnauthorized 判断 err 是否是 Unauthorized 错误.
func IsUnauthorized(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusUnauthorized &&
		err.Message == "Unauthorized"
}

// Forbidden creates a new Forbidden error. It means authenticated but not
// allowed | Forbidden 创建一个新的 Forbidden 错误.
// 未授权（已认证但无权限）。
func Forbidden(status string) *Error {
	return New(http.StatusForbidden, status).WithMessage("Forbidden")
}

// IsForbidden determines if err is Forbidden error | IsForbidden 判断 err 是否是 Forbidden 错误.
func IsForbidden(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusForbidden &&
		err.Message == "Forbidden"
}

// NotFound creates a new NotFound error | NotFound 创建一个新的 NotFound 错误.
func NotFound(status string) *Error {
	return New(http.StatusNotFound, status).WithMessage("Not Found")
}

// IsNotFound determines if err is NotFound error | IsNotFound 判断 err 是否是 NotFound 错误.
func IsNotFound(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusNotFound &&
		err.Message == "Not Found"
}

// Conflict creates a new Conflict error | Conflict 创建一个新的 Conflict 错误.
func Conflict(status string) *Error {
	return New(http.StatusConflict, status).WithMessage("Conflict")
}

// IsConflict determines if err is Conflict error | IsConflict 判断 err 是否是 Conflict 错误.
func IsConflict(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusConflict &&
		err.Message == "Conflict"
}

// InternalServer creates a new InternalServer error | InternalServer 创建一个新的 InternalServer 错误.
func InternalServer(status string) *Error {
	return New(http.StatusInternalServerError, status).WithMessage("Internal Server Error")
}

// IsInternalServer determines if err is InternalServer error | IsInternalServer 判断 err 是否是 InternalServer 错误.
func IsInternalServer(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusInternalServerError &&
		err.Message == "Internal Server Error"
}

// ServiceUnavailable creates a new ServiceUnavailable error | ServiceUnavailable 创建一个新的 ServiceUnavailable 错误.
func ServiceUnavailable(status string) *Error {
	return New(http.StatusServiceUnavailable, status).WithMessage("Service Unavailable")
}

// IsServiceUnavailable determines if err is ServiceUnavailable error | IsServiceUnavailable 判断 err 是否是 ServiceUnavailable 错误.
func IsServiceUnavailable(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusServiceUnavailable &&
		err.Message == "Service Unavailable"
}

// GatewayTimeout creates a new GatewayTimeout error | GatewayTimeout 创建一个新的 GatewayTimeout 错误.
func GatewayTimeout(status string) *Error {
	return New(http.StatusGatewayTimeout, status).WithMessage("Gateway Timeout")
}

// IsGatewayTimeout determines if err is GatewayTimeout error | IsGatewayTimeout 判断 err 是否是 GatewayTimeout 错误.
func IsGatewayTimeout(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusGatewayTimeout &&
		err.Message == "Gateway Timeout"
}

// ClientClosed creates a new ClientClosed error. Note: 499 is the non-standard
// status code defined by nginx (client closed the connection)
// ClientClosed 创建一个新的 ClientClosed 错误.
// 注意：499 是Nginx定义的非标准状态码（客户端主动关闭连接）。
func ClientClosed(status string) *Error {
	return New(499, status).WithMessage("Client Closed")
}

// IsClientClosed determines if err is ClientClosed error | IsClientClosed 判断 err 是否是 ClientClosed 错误.
func IsClientClosed(err *Error) bool {
	return err != nil &&
		err.Code == 499 &&
		err.Message == "Client Closed"
}

// TooManyRequests creates a new TooManyRequests error | TooManyRequests 创建一个新的 TooManyRequests 错误.
func TooManyRequests(status string) *Error {
	return New(http.StatusTooManyRequests, status).WithMessage("Too Many Requests")
}

// IsTooManyRequests determines if err is TooManyRequests error | IsTooManyRequests 判断 err 是否是 TooManyRequests 错误.
func IsTooManyRequests(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusTooManyRequests &&
		err.Message == "Too Many Requests"
}

// TokenInvalid reports an invalid token | TokenInvalid 表示 token 无效.
func TokenInvalid(status string) *Error {
	return New(http.StatusUnauthorized, status).WithMessage("Token Invalid")
}

// IsTokenInvalid determines if err is TokenInvalid error | IsTokenInvalid 判断 err 是否是 TokenInvalid 错误.
func IsTokenInvalid(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusUnauthorized &&
		err.Message == "Token Invalid"
}

// TokenExpired reports an expired token | TokenExpired 表示 token 过期.
func TokenExpired(status string) *Error {
	return New(http.StatusUnauthorized, status).WithMessage("Token Expired")
}

// IsTokenExpired determines if err is TokenExpired error | IsTokenExpired 判断 err 是否是 TokenExpired 错误.
func IsTokenExpired(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusUnauthorized &&
		err.Message == "Token Expired"
}

// TokenInvalidSignature reports an invalid token signature, typically for
// failed JWT or other token verification | TokenInvalidSignature 表示 token 签名无效.
// 通常用于处理 JWT 或其他类型的令牌验证失败的情况。
func TokenInvalidSignature(status string) *Error {
	return New(http.StatusUnauthorized, status).WithMessage("Token Invalid Signature")
}

// IsTokenInvalidSignature determines if err is TokenInvalidSignature error | IsTokenInvalidSignature 判断 err 是否是 TokenInvalidSignature 错误.
func IsTokenInvalidSignature(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusUnauthorized &&
		err.Message == "Token Invalid Signature"
}

// BindError reports a bind error, typically for request body or query
// parameter parsing failures | BindError 表示绑定参数错误.
// 通常用于处理请求体或查询参数的解析错误。
func BindError(status string) *Error {
	return New(http.StatusBadRequest, status).WithMessage("Bind Error")
}

// IsBindError determines if err is BindError error | IsBindError 判断 err 是否是 BindError 错误.
func IsBindError(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusBadRequest &&
		err.Message == "Bind Error"
}

// InvalidArguments reports invalid arguments, i.e. bad arguments on a
// function or method call | InvalidArguments 表示无效参数错误（函数/方法调用时的参数错误）.
func InvalidArguments(status string) *Error {
	return New(http.StatusBadRequest, status).WithMessage("Invalid Arguments")
}

// IsInvalidArguments determines if err is InvalidArguments error | IsInvalidArguments 判断 err 是否是 InvalidArguments 错误.
func IsInvalidArguments(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusBadRequest &&
		err.Message == "Invalid Arguments"
}

// InvalidParams reports invalid parameters, i.e. failed validation of
// external input | InvalidParams 表示无效参数错误（外部输入参数验证失败）.
func InvalidParams(status string) *Error {
	return New(http.StatusBadRequest, status).WithMessage("Invalid Params")
}

// IsInvalidParams determines if err is InvalidParams error | IsInvalidParams 判断 err 是否是 InvalidParams 错误.
func IsInvalidParams(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusBadRequest &&
		err.Message == "Invalid Params"
}

// PanicError reports a runtime panic, e.g. nil pointer or out-of-range index
// PanicError 表示 panic 错误.
// 通常用于处理程序运行时的异常情况（如空指针、数组越界）。
func PanicError(status string) *Error {
	return New(http.StatusInternalServerError, status).WithMessage("Panic Error")
}

// IsPanicError determines if err is PanicError error | IsPanicError 判断 err 是否是 PanicError 错误.
func IsPanicError(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusInternalServerError &&
		err.Message == "Panic Error"
}

// PageNotFound reports a missing page or resource | PageNotFound 表示页面未找到.
// 通常用于处理请求的页面或资源未找到的情况。
func PageNotFound(status string) *Error {
	return New(http.StatusNotFound, status).WithMessage("Page Not Found")
}

// IsPageNotFound determines if err is PageNotFound error | IsPageNotFound 判断 err 是否是 PageNotFound 错误.
func IsPageNotFound(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusNotFound &&
		err.Message == "Page Not Found"
}

// DBReadError reports a database read failure, covering SELECT/FETCH queries
// DBReadError 表示数据库读错误.
// 包含 SELECT/FETCH 等查询操作失败。
func DBReadError(status string) *Error {
	return New(http.StatusInternalServerError, status).WithMessage("DB Read Error")
}

// IsDBReadError determines if err is DBReadError error | IsDBReadError 判断 err 是否是 DBReadError 错误.
func IsDBReadError(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusInternalServerError &&
		err.Message == "DB Read Error"
}

// DBWriteError reports a database write failure, covering INSERT/UPDATE/DELETE
// DBWriteError 表示数据库写错误.
// 包含 INSERT/UPDATE/DELETE 等写入操作失败。
func DBWriteError(status string) *Error {
	return New(http.StatusInternalServerError, status).WithMessage("DB Write Error")
}

// IsDBWriteError determines if err is DBWriteError error | IsDBWriteError 判断 err 是否是 DBWriteError 错误.
func IsDBWriteError(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusInternalServerError &&
		err.Message == "DB Write Error"
}

// DBTransactionError reports a failed database transaction
// (BEGIN/COMMIT/ROLLBACK) | DBTransactionError 表示数据库事务错误.
// 数据库事务操作（BEGIN/COMMIT/ROLLBACK）失败。
func DBTransactionError(status string) *Error {
	return New(http.StatusInternalServerError, status).WithMessage("DB Transaction Error")
}

// IsDBTransactionError determines if err is DBTransactionError error | IsDBTransactionError 判断 err 是否是 DBTransactionError 错误.
func IsDBTransactionError(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusInternalServerError &&
		err.Message == "DB Transaction Error"
}

// PermissionDenied reports that the request lacks permission | PermissionDenied 表示请求没有权限.
func PermissionDenied(status string) *Error {
	return New(http.StatusForbidden, status).WithMessage("Permission Denied")
}

// IsPermissionDenied determines if err is the permission denied error | IsPermissionDenied 判断是否是权限不足的错误.
func IsPermissionDenied(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusForbidden &&
		err.Message == "Permission Denied"
}

// OperationFailed reports a failed operation | OperationFailed 表示操作失败.
func OperationFailed(status string) *Error {
	return New(http.StatusInternalServerError, status).WithMessage("Operation Failed")
}

// IsOperationFailed determines if err is the operation failed error | IsOperationFailed 判断是否是操作失败的错误.
func IsOperationFailed(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusInternalServerError &&
		err.Message == "Operation Failed"
}

// Unauthenticated reports that the request is not authenticated. Typical
// case: an API requires credentials but none valid was provided
// Unauthenticated 表示请求未经过身份验证.
// 典型场景：API 需要认证但未提供有效凭证。
func Unauthenticated(status string) *Error {
	return New(http.StatusUnauthorized, status).WithMessage("Unauthenticated")
}

// IsUnauthenticated determines if err is the unauthenticated error | IsUnauthenticated 判断是否是未认证错误.
func IsUnauthenticated(err *Error) bool {
	return err != nil &&
		err.Code == http.StatusUnauthorized &&
		err.Message == "Unauthenticated"
}
