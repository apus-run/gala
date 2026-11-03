// Package http converts HTTP status codes to gRPC codes and back — the
// transport mapping used by errorsx-based services |
// http 在 HTTP 状态码与 gRPC 状态码之间双向转换，是 errorsx 体系使用的传输层映射.
//
// # Notes | 说明
//
// Mappings follow google.rpc code.proto; unknown codes degrade to
// codes.Unknown / 500. nginx's non-standard 499 is kept as [ClientClosed]
// and maps to codes.Canceled. Replace [DefaultConverter] to plug in a
// custom mapping for the package-level functions.
// 映射遵循 google.rpc code.proto；未知码分别退化为 codes.Unknown / 500。
// nginx 的非标准 499 以 [ClientClosed] 保留，映射为 codes.Canceled。替换
// [DefaultConverter] 可为包级函数注入自定义映射。
//
// # Example | 示例
//
//	code := http.ToGRPCCode(http.StatusNotFound) // codes.NotFound
//	status := http.FromGRPCCode(codes.Unavailable) // 503
package http

import (
	"net/http"

	"google.golang.org/grpc/codes"
)

const (
	// ClientClosed is non-standard http status code,
	// which defined by nginx. https://httpstatus.in/499/
	// ClientClosed 是非标准 HTTP 状态码，由 nginx 定义。
	ClientClosed = 499
)

// Converter is a status converter | Converter 是状态码转换器.
type Converter interface {
	// ToGRPCCode converts an HTTP error code into the corresponding gRPC response status
	// ToGRPCCode 把 HTTP 错误码转换为对应的 gRPC 响应状态.
	ToGRPCCode(code int) codes.Code

	// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status
	// FromGRPCCode 把 gRPC 错误码转换为对应的 HTTP 响应状态.
	FromGRPCCode(code codes.Code) int
}

type statusConverter struct{}

// DefaultConverter is the default converter | DefaultConverter 默认转换器.
var DefaultConverter Converter = statusConverter{}

// ToGRPCCode converts a HTTP error code into the corresponding gRPC response status.
// See: https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto
// ToGRPCCode 把 HTTP 错误码转换为对应的 gRPC 响应状态。
// 参见: https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto
func (c statusConverter) ToGRPCCode(code int) codes.Code {
	switch code {
	case http.StatusOK:
		return codes.OK
	case http.StatusBadRequest:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.Aborted
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case http.StatusInternalServerError:
		return codes.Internal
	case http.StatusNotImplemented:
		return codes.Unimplemented
	case http.StatusServiceUnavailable:
		return codes.Unavailable
	case http.StatusGatewayTimeout:
		return codes.DeadlineExceeded
	case ClientClosed:
		return codes.Canceled
	}
	return codes.Unknown
}

// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status.
// See: https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto
// FromGRPCCode 把 gRPC 错误码转换为对应的 HTTP 响应状态。
// 参见: https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto
func (c statusConverter) FromGRPCCode(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.Canceled:
		return ClientClosed
	case codes.Unknown:
		return http.StatusInternalServerError
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.FailedPrecondition:
		return http.StatusBadRequest
	case codes.Aborted:
		return http.StatusConflict
	case codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Internal:
		return http.StatusInternalServerError
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DataLoss:
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

// ToGRPCCode converts an HTTP error code into the corresponding gRPC response status
// ToGRPCCode 把 HTTP 错误码转换为对应的 gRPC 响应状态.
func ToGRPCCode(code int) codes.Code {
	return DefaultConverter.ToGRPCCode(code)
}

// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status
// FromGRPCCode 把 gRPC 错误码转换为对应的 HTTP 响应状态.
func FromGRPCCode(code codes.Code) int {
	return DefaultConverter.FromGRPCCode(code)
}
