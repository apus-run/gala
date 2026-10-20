package s3

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/smithy-go"

	"github.com/apus-run/gala/components/fileserver"
)

// statusCoder 由 SDK 的 HTTP 响应错误实现。
type statusCoder interface {
	HTTPStatusCode() int
}

func opErr(op, key string, err error) error {
	return &fileserver.Error{Op: op, Key: key, Err: err}
}

func wrap(sentinel, cause error) error {
	return fmt.Errorf("%w: %w", sentinel, cause)
}

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

// hasResponse 报告错误是否来自一个 HTTP 响应。没有响应时，请求可能没有到达服务端，也可能已被处理。
func hasResponse(err error) bool {
	var sc statusCoder
	return errors.As(err, &sc) && sc.HTTPStatusCode() > 0
}

// classify 把 SDK 错误映射为哨兵类别（见设计文档 6.5），同时保留原始原因。
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
	case code == "PreconditionFailed" || code == "ConditionalRequestConflict" ||
		status == http.StatusPreconditionFailed || status == http.StatusConflict:
		return wrap(fileserver.ErrConflict, err)
	case code == "EntityTooLarge" || status == http.StatusRequestEntityTooLarge:
		return wrap(fileserver.ErrTooLarge, err)
	case code == "NotImplemented" || status == http.StatusNotImplemented:
		// 服务端没有实现这个功能（例如不支持 If-None-Match 的兼容服务），请求没有被处理。
		return wrap(fileserver.ErrUnsupported, err)
	}
	return err
}

// classifyWrite 用于可能已经生效的写入请求（PutObject、UploadPart、CompleteMultipartUpload）。
// 没有收到 HTTP 响应的失败（连接错误、超时、取消）结果未知，映射为 ErrIndeterminate，原因保留在错误链中。
// 收到 5xx 也不代表服务端没有提交：网关错误、后端超时都可能是“已写入但响应失败”，
// 因此同样按结果未知处理。501 是例外：服务端没有实现该功能，请求确定没有被处理。
func classifyWrite(err error) error {
	if !hasResponse(err) {
		return wrap(fileserver.ErrIndeterminate, err)
	}
	if _, status := apiError(err); status >= http.StatusInternalServerError && status != http.StatusNotImplemented {
		return wrap(fileserver.ErrIndeterminate, err)
	}
	return classify(err)
}

// classifyComplete 在 classifyWrite 的基础上处理：Complete 返回 NoSuchUpload 时，
// 任务已经结束但结果未知，因此返回 ErrIndeterminate。
func classifyComplete(err error) error {
	if code, _ := apiError(err); code == "NoSuchUpload" {
		return wrap(fileserver.ErrIndeterminate, err)
	}
	return classifyWrite(err)
}
