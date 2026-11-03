package resend

import (
	"context"
	"errors"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	resendgo "github.com/resend/resend-go/v3"

	"github.com/apus-run/gala/components/mail"
)

// mapError classifies failures raised after the Client call. In resend-go v3
// only HTTP 429 surfaces as an identifiable *RateLimitError; every other HTTP
// status degrades to a plain error, so no status guessing or error-string
// parsing happens and SendOutcomeUnknown is used conservatively.
// mapError 分类调用 Client 之后的失败。resend-go v3 只有 HTTP 429
// 返回可识别的 *RateLimitError；其他 HTTP 状态都退化成普通错误，
// 因此不猜测状态码，也不解析错误字符串，保守使用 SendOutcomeUnknown。
func mapError(err error) error {
	// Recognize in-flight Context errors first so errors.Is stays usable.
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

	// 429: Resend did not accept the request. A 429 may also mean the daily or
	// monthly quota is exhausted; a zero RetryAfter must not be read as
	// "retry immediately".
	// 429：Resend 未接受请求。429 也可能表示日/月额度耗尽，
	// RetryAfter 为 0 时不得解释为立即重试。
	if rateLimitErr, ok := errors.AsType[*resendgo.RateLimitError](err); ok {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "rate_limited",
			IsTransient: true,
			Outcome:     mail.SendOutcomeNotAccepted,
			RetryAfter:  parseRetryAfter(rateLimitErr.RetryAfter),
			Err:         err,
		}
	}
	if errors.Is(err, resendgo.ErrRateLimit) {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "rate_limited",
			IsTransient: true,
			Outcome:     mail.SendOutcomeNotAccepted,
			Err:         err,
		}
	}

	// A transport timeout may lose the response after the request was already
	// accepted; other network errors (such as a permanent DNS failure) do not
	// enter the transient classification.
	// 传输超时可能在请求已被接受后丢失响应；其他网络错误
	// （如永久 DNS 失败）不进入瞬态分类。
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "timeout",
			IsTransient: true,
			Outcome:     mail.SendOutcomeUnknown,
			Err:         err,
		}
	}

	return &mail.DeliveryError{
		Provider:    Provider,
		IsTransient: false,
		Outcome:     mail.SendOutcomeUnknown,
		Err:         err,
	}
}

// parseRetryAfter parses Retry-After as decimal seconds only. Missing,
// invalid, non-positive or unrepresentable values keep 0, meaning "no reliable
// hint" rather than "retry immediately".
// parseRetryAfter 仅按十进制秒解析 Retry-After；缺失、非法、
// 非正数或无法表示为 time.Duration 时保持 0（表示没有可靠提示，
// 而不是立即重试）。
func parseRetryAfter(raw string) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || seconds <= 0 || seconds > math.MaxInt64/int64(time.Second) {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
