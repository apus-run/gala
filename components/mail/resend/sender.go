package resend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	resendgo "github.com/resend/resend-go/v3"

	"github.com/apus-run/gala/components/mail"
)

// Provider 是本适配器写入 Receipt 与 DeliveryError 的标识。
const Provider = "resend"

// DefaultMaxMessageBytes 是近似原始输入的默认消息预算。
// 它是独立于 Provider 配额的进程内安全上限：resend-go 序列化附件时
// 峰值内存可达原始内容的数倍，预算在任何复制与序列化之前生效。
const DefaultMaxMessageBytes int64 = 10 << 20 // 10 MiB

// Client 是适配器需要的最窄 Resend 能力，*resendgo.Client 的
// Emails 服务直接满足该接口。
type Client interface {
	SendWithOptions(
		ctx context.Context,
		params *resendgo.SendEmailRequest,
		options *resendgo.SendEmailOptions,
	) (*resendgo.SendEmailResponse, error)
}

// Option 配置 Sender。
type Option func(*Sender)

// WithMaxMessageBytes 调整近似原始消息预算。提高预算不能绕过
// Resend 的硬限制；应用还必须按该预算限制并发发送数。
func WithMaxMessageBytes(n int64) Option {
	return func(s *Sender) { s.maxMessageBytes = n }
}

// Sender 通过 Resend 发送邮件。构造后不可变，可并发使用。
type Sender struct {
	client          Client
	maxMessageBytes int64
}

var _ mail.Sender = (*Sender)(nil)

// New 用注入的窄 Client 构造 Sender。
func New(client Client, options ...Option) (*Sender, error) {
	if client == nil {
		return nil, errors.New("mail/resend: nil client")
	}
	s := &Sender{client: client, maxMessageBytes: DefaultMaxMessageBytes}
	for _, option := range options {
		option(s)
	}
	if s.maxMessageBytes <= 0 {
		return nil, errors.New("mail/resend: max message bytes must be positive")
	}
	return s, nil
}

// NewFromAPIKey 是简单场景的便捷入口，沿用 resend-go 默认 Client 行为
// （包括 SDK 自身的环境变量约定）。需要自定义 HTTP Client、Base URL
// 或观测配置时应构造官方 Client 后使用 New。
func NewFromAPIKey(apiKey string, options ...Option) (*Sender, error) {
	apiKey = strings.TrimSpace(strings.Trim(strings.TrimSpace(apiKey), "'"))
	if apiKey == "" {
		return nil, errors.New("mail/resend: empty api key")
	}
	return New(resendgo.NewClient(apiKey).Emails, options...)
}

// SendOptions 携带 Resend 特有的发送选项。
type SendOptions struct {
	// IdempotencyKey 非空时启用 Resend 幂等发送：1～256 个字符，
	// 不得含控制字符。同一 Key 的所有重试必须使用完全相同的 payload；
	// Resend 只在 24 小时内保留幂等结果。
	IdempotencyKey string
}

// Send 实现 mail.Sender。
func (s *Sender) Send(ctx context.Context, msg mail.Message) (mail.Receipt, error) {
	return s.SendWithOptions(ctx, msg, SendOptions{})
}

// SendWithOptions 在公共 Send 语义之上附加 Resend 特有选项。
func (s *Sender) SendWithOptions(
	ctx context.Context,
	msg mail.Message,
	options SendOptions,
) (mail.Receipt, error) {
	// 契约（设计文档 §19.2）：Context 已取消时不调用 Client，原样返回。
	if err := ctx.Err(); err != nil {
		return mail.Receipt{}, err
	}

	if err := msg.Validate(); err != nil {
		return mail.Receipt{}, err
	}

	if err := validateProviderMessage(msg); err != nil {
		return mail.Receipt{}, err
	}

	if err := validateSendOptions(options); err != nil {
		return mail.Receipt{}, err
	}

	if err := validateMessageSize(msg, s.maxMessageBytes); err != nil {
		return mail.Receipt{}, err
	}

	request := mapMessage(msg)

	// 映射附件可能耗时；调用 Client 前再次检查，避免已取消请求继续发信。
	if err := ctx.Err(); err != nil {
		return mail.Receipt{}, err
	}

	output, err := s.client.SendWithOptions(ctx, request, &resendgo.SendEmailOptions{
		IdempotencyKey: options.IdempotencyKey,
	})
	if err != nil {
		return mail.Receipt{}, mapError(err)
	}
	if output == nil {
		return mail.Receipt{}, &mail.DeliveryError{
			Provider: Provider,
			Code:     "invalid_response",
			Outcome:  mail.SendOutcomeUnknown,
			Err:      errors.New("resend returned a nil response"),
		}
	}
	if output.Id == "" {
		return mail.Receipt{}, &mail.DeliveryError{
			Provider: Provider,
			Code:     "invalid_response",
			Outcome:  mail.SendOutcomeUnknown,
			Err:      errors.New("resend returned an empty message id"),
		}
	}

	return mail.Receipt{
		Provider:  Provider,
		MessageID: output.Id,
	}, nil
}

const maxIdempotencyKeyLength = 256

func validateSendOptions(options SendOptions) error {
	key := options.IdempotencyKey
	if key == "" {
		return nil
	}
	if len(key) > maxIdempotencyKeyLength {
		return fmt.Errorf("%w: resend idempotency key longer than %d characters",
			mail.ErrInvalidMessage, maxIdempotencyKeyLength)
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: resend idempotency key contains control characters",
				mail.ErrInvalidMessage)
		}
	}
	return nil
}
