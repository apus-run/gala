package mail

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidMessage 表示消息未通过公共校验或 Provider 前置校验，
// 未向 Provider 发起请求。使用 errors.Is 识别。
var ErrInvalidMessage = errors.New("mail: invalid message")

// SendOutcome 表示 Send 返回错误时，Provider 是否可能已经接受请求。
type SendOutcome uint8

const (
	// SendOutcomeUnknown 是保守默认值：请求可能未到达，也可能已被接受。
	SendOutcomeUnknown SendOutcome = iota
	// SendOutcomeNotAccepted 表示已知 Provider 未接受请求。
	SendOutcomeNotAccepted
)

// DeliveryError 表示向 Provider 提交发送请求失败。
//
// IsTransient 只表示同类故障之后可能恢复，不表示本次发送可以
// 无重复风险地自动重试；重试决策还必须结合 Outcome。
type DeliveryError struct {
	// Provider 是发生故障的 Provider 标识。
	Provider string
	// Code 是 Provider 或分类器给出的有界错误码；可能为空。
	Code string
	// IsTransient 表示同类故障之后可能恢复。
	IsTransient bool
	// Outcome 表示 Provider 是否可能已接受请求；零值 Unknown 为保守默认。
	Outcome SendOutcome
	// RetryAfter 大于 0 时是下一次尝试的最小等待时间；0 表示无可靠提示。
	RetryAfter time.Duration
	// Err 保留官方 SDK 的原始错误，允许调用方继续 errors.As。
	Err error
}

func (e *DeliveryError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("mail: %s send failed: %v", e.Provider, e.Err)
	}
	return fmt.Sprintf("mail: %s send failed (%s): %v", e.Provider, e.Code, e.Err)
}

func (e *DeliveryError) Unwrap() error { return e.Err }

// IsTransient 报告 err 是否为瞬态投递故障。
// 瞬态只表示同类故障之后可能恢复，不构成安全重试的充分条件。
func IsTransient(err error) bool {
	var deliveryErr *DeliveryError
	return errors.As(err, &deliveryErr) && deliveryErr.IsTransient
}

// MayHaveBeenAccepted 报告 Provider 是否可能已接受本次请求。
// 对任何 DeliveryError 采用保守判断；非 DeliveryError
// （公共校验失败、调用 Client 前取消）没有发起 Provider 请求。
func MayHaveBeenAccepted(err error) bool {
	var deliveryErr *DeliveryError
	return errors.As(err, &deliveryErr) &&
		deliveryErr.Outcome != SendOutcomeNotAccepted
}
