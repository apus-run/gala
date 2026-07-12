package ses

import (
	"context"
	"errors"
	"net"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/apus-run/gala/components/mail"
)

// transientCodes 是按 SES 语义可能恢复的错误码。
// LimitExceededException 被 AWS Retryer 视为 throttling；
// 它表示资源数量限制，不等同于每日发送配额。
var transientCodes = map[string]struct{}{
	"TooManyRequestsException": {},
	"LimitExceededException":   {},
}

// mapError 分类调用 Client 之后的失败。先用 errors.As 获取
// smithy.APIError 的 ErrorCode()，再用 *smithyhttp.ResponseError 判断
// 最终 HTTP 状态，不只枚举具体异常类型（SendEmail 的反序列化器不会
// 把最终 5xx 解码成 typed exception）。
//
// AWS Client 的 Retryer 由应用注入且最终错误不暴露完整尝试历史，
// 因此所有调用后的失败均为 SendOutcomeUnknown。
func mapError(err error) error {
	// 先识别调用中的 Context 错误，保持 errors.Is 可达。
	if errors.Is(err, context.Canceled) {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "canceled",
			IsTransient: false,
			Outcome:     mail.SendOutcomeUnknown,
			Err:         err,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "deadline_exceeded",
			IsTransient: true,
			Outcome:     mail.SendOutcomeUnknown,
			Err:         err,
		}
	}

	code := ""
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code = apiErr.ErrorCode()
	}

	transient := false
	if _, ok := transientCodes[code]; ok {
		transient = true
	}

	// 最终 HTTP 5xx（含未 typed 的 InternalServiceError）视为瞬态。
	var responseErr *smithyhttp.ResponseError
	if !transient && errors.As(err, &responseErr) && responseErr.HTTPStatusCode() >= 500 {
		transient = true
		if code == "" {
			code = "http_5xx"
		}
	}

	// 传输超时可能在请求已被接受后丢失响应；其他网络错误
	// （如永久 DNS 失败）不进入瞬态分类。
	var netErr net.Error
	if !transient && code == "" && errors.As(err, &netErr) && netErr.Timeout() {
		transient = true
		code = "timeout"
	}

	return &mail.DeliveryError{
		Provider:    Provider,
		Code:        code,
		IsTransient: transient,
		Outcome:     mail.SendOutcomeUnknown,
		Err:         err,
	}
}
