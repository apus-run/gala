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

// mapError 分类调用 Client 之后的失败。resend-go v3 只有 HTTP 429
// 返回可识别的 *RateLimitError；其他 HTTP 状态都退化成普通错误，
// 因此不猜测状态码，也不解析错误字符串，保守使用 SendOutcomeUnknown。
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

	// 429：Resend 未接受请求。429 也可能表示日/月额度耗尽，
	// RetryAfter 为 0 时不得解释为立即重试。
	var rateLimitErr *resendgo.RateLimitError
	if errors.As(err, &rateLimitErr) {
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

	// 传输超时可能在请求已被接受后丢失响应；其他网络错误
	// （如永久 DNS 失败）不进入瞬态分类。
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
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
