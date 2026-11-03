package s3

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/smithy-go"

	"github.com/apus-run/gala/components/fileserver"
)

// statusCoder is implemented by the SDK's HTTP response errors | statusCoder 由 SDK 的 HTTP 响应错误实现。
type statusCoder interface {
	HTTPStatusCode() int
}

func opErr(op, key string, err error) error {
	return &fileserver.Error{Op: op, Key: key, Err: err}
}

func wrap(sentinel, cause error) error {
	return fmt.Errorf("%w: %w", sentinel, cause)
}

// apiError extracts the S3 error code and HTTP status code, returning
// zero values when neither is present.
// apiError 提取 S3 错误码与 HTTP 状态码。没有对应信息时返回零值。
func apiError(err error) (code string, status int) {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}
	var sc statusCoder
	if errors.As(err, &sc) {
		status = sc.HTTPStatusCode()
	}
	return code, status
}

// classify maps SDK errors to sentinel categories while keeping the
// original cause. Errors outside the mapping table are returned as-is.
// classify 把 SDK 错误映射为哨兵类别，同时保留原始原因。
// 映射表之外的错误原样返回。
func classify(err error) error {
	if err == nil {
		return nil
	}
	code, status := apiError(err)
	switch {
	case code == "NoSuchBucket":
		return err
	case code == "NoSuchKey" || code == "NotFound" || status == http.StatusNotFound:
		return wrap(fileserver.ErrNotFound, err)
	case code == "AccessDenied" || code == "Forbidden" || status == http.StatusForbidden:
		return wrap(fileserver.ErrPermission, err)
	case code == "SlowDown" || code == "ThrottlingException" || status == http.StatusTooManyRequests:
		// The request was throttled before being processed, so retrying
		// is safe. SlowDown on the write path is a 5xx and classifyWrite
		// still treats the result as unknown.
		// 请求在处理前被限流拒绝，重试安全。写路径的 SlowDown 是 5xx，classifyWrite 仍按结果未知处理。
		return wrap(fileserver.ErrThrottled, err)
	case code == "PreconditionFailed" || code == "ConditionalRequestConflict" ||
		status == http.StatusPreconditionFailed || status == http.StatusConflict:
		return wrap(fileserver.ErrConflict, err)
	case code == "EntityTooLarge" || status == http.StatusRequestEntityTooLarge:
		return wrap(fileserver.ErrTooLarge, err)
	case code == "NotImplemented" || status == http.StatusNotImplemented:
		// The server does not implement this feature (e.g. a compatible
		// service without If-None-Match support); the request was not
		// processed.
		// 服务端没有实现这个功能（例如不支持 If-None-Match 的兼容服务），请求没有被处理。
		return wrap(fileserver.ErrUnsupported, err)
	}
	return err
}

// classifyWrite applies to write requests that may already have taken
// effect (PutObject, CreateMultipartUpload, CompleteMultipartUpload).
// Failures with no HTTP response (connection error, timeout, cancellation)
// have an unknown result and map to ErrIndeterminate, with the cause kept
// in the error chain. A 5xx response does not mean the server skipped the
// commit either: gateway errors and backend timeouts can all be "written
// but the response failed", so they are likewise treated as unknown. 501
// is the exception: the server does not implement the feature and the
// request definitely was not processed.
// classifyWrite 用于可能已经生效的写入请求（PutObject、CreateMultipartUpload、CompleteMultipartUpload）。
// 没有收到 HTTP 响应的失败（连接错误、超时、取消）结果未知，映射为 ErrIndeterminate，原因保留在错误链中。
// 收到 5xx 也不代表服务端没有提交：网关错误、后端超时都可能是“已写入但响应失败”，
// 因此同样按结果未知处理。501 是例外：服务端没有实现该功能，请求确定没有被处理。
func classifyWrite(err error) error {
	_, status := apiError(err)
	if status <= 0 || (status >= http.StatusInternalServerError && status != http.StatusNotImplemented) {
		return wrap(fileserver.ErrIndeterminate, err)
	}
	return classify(err)
}

// classifyComplete extends classifyWrite: when Complete returns
// NoSuchUpload the task has already ended with an unknown result, so it
// returns ErrIndeterminate.
// classifyComplete 在 classifyWrite 的基础上处理：Complete 返回 NoSuchUpload 时，
// 任务已经结束但结果未知，因此返回 ErrIndeterminate。
func classifyComplete(err error) error {
	if code, _ := apiError(err); code == "NoSuchUpload" {
		return wrap(fileserver.ErrIndeterminate, err)
	}
	return classifyWrite(err)
}
