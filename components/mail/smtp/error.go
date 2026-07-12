package smtp

import (
	"context"
	"errors"
	"net"
	"syscall"

	gomail "github.com/wneessen/go-mail"

	"github.com/apus-run/gala/components/mail"
)

// mapDialError 分类拨号阶段（TCP、TLS、EHLO、认证）的失败。拨号期间
// 不可能发生邮件事务，Outcome 一律 SendOutcomeNotAccepted（SPEC D7），
// 使服务器暂时不可达能进入调用方的通用安全重试路径。
func mapDialError(err error) error {
	if errors.Is(err, context.Canceled) {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "canceled",
			IsTransient: false,
			Outcome:     mail.SendOutcomeNotAccepted,
			Err:         err,
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        "deadline_exceeded",
			IsTransient: true,
			Outcome:     mail.SendOutcomeNotAccepted,
			Err:         err,
		}
	}

	// 瞬态白名单：传输超时与连接被拒（服务器重启中）。TLS 配置、
	// 认证失败、永久 DNS 错误等保持非瞬态。
	transient := false
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		transient = true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		transient = true
	}
	return &mail.DeliveryError{
		Provider:    Provider,
		Code:        "dial",
		IsTransient: transient,
		Outcome:     mail.SendOutcomeNotAccepted,
		Err:         err,
	}
}

// notAcceptedReasons 是 DATA 终止序列（<CRLF>.<CRLF>）发出之前的失败：
// 按 RFC 5321，服务器此时不能接受该消息。
var notAcceptedReasons = map[gomail.SendErrReason]string{
	gomail.ErrGetSender:    "get_sender",
	gomail.ErrGetRcpts:     "get_rcpts",
	gomail.ErrNoUnencoded:  "no_unencoded",
	gomail.ErrConnCheck:    "conn_check",
	gomail.ErrSMTPMailFrom: "mail_from",
	gomail.ErrSMTPRcptTo:   "rcpt_to",
	gomail.ErrSMTPData:     "data",
	gomail.ErrWriteContent: "write_content",
}

// unknownOutcomeReasons 是终止序列已（可能）发出后的失败：响应错误
// 或丢失，消息可能已被接受，保守 SendOutcomeUnknown。
var unknownOutcomeReasons = map[gomail.SendErrReason]string{
	gomail.ErrSMTPDataClose: "data_close",
	gomail.ErrSMTPReset:     "reset",
	gomail.ErrAmbiguous:     "ambiguous",
}

// mapSendError 分类发送阶段的失败。判定次序（SPEC §6.1）：先 Context
// 错误（契约要求保持 errors.Is 且 Outcome Unknown），再按
// SendError.Reason 分阶段，再网络超时，最后保守默认。
func mapSendError(err error) error {
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

	var sendErr *gomail.SendError
	if errors.As(err, &sendErr) {
		if code, ok := notAcceptedReasons[sendErr.Reason]; ok {
			return &mail.DeliveryError{
				Provider:    Provider,
				Code:        code,
				IsTransient: sendErr.IsTemp(),
				Outcome:     mail.SendOutcomeNotAccepted,
				Err:         err,
			}
		}
		code, ok := unknownOutcomeReasons[sendErr.Reason]
		if !ok {
			// go-mail 未来新增的 Reason：保守 Unknown。
			code = "ambiguous"
		}
		return &mail.DeliveryError{
			Provider:    Provider,
			Code:        code,
			IsTransient: sendErr.IsTemp(),
			Outcome:     mail.SendOutcomeUnknown,
			Err:         err,
		}
	}

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
