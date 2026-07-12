package ses

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	"github.com/apus-run/gala/components/mail"
)

// Provider 是本适配器写入 Receipt 与 DeliveryError 的标识。
const Provider = "ses"

// Client 是适配器需要的最窄 SES 能力，*sesv2.Client 直接满足该接口。
type Client interface {
	SendEmail(
		ctx context.Context,
		params *sesv2.SendEmailInput,
		optFns ...func(*sesv2.Options),
	) (*sesv2.SendEmailOutput, error)
}

// Sender 通过 Amazon SES API v2 发送邮件。构造后不可变，可并发使用。
type Sender struct {
	client           Client
	configurationSet string
	maxMessageBytes  int64
}

var _ mail.Sender = (*Sender)(nil)

// New 用注入的窄 Client 构造 Sender。适配器不调用
// config.LoadDefaultConfig，也不读取环境变量。
func New(client Client, options ...Option) (*Sender, error) {
	if client == nil {
		return nil, errors.New("mail/ses: nil client")
	}
	s := &Sender{client: client, maxMessageBytes: DefaultMaxMessageBytes}
	for _, option := range options {
		option(s)
	}
	if s.maxMessageBytes <= 0 {
		return nil, errors.New("mail/ses: max message bytes must be positive")
	}
	if err := validateConfigurationSetName(s.configurationSet); err != nil {
		return nil, err
	}
	return s, nil
}

// Send 实现 mail.Sender。
func (s *Sender) Send(ctx context.Context, msg mail.Message) (mail.Receipt, error) {
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

	if err := validateMessageSize(msg, s.maxMessageBytes); err != nil {
		return mail.Receipt{}, err
	}

	input := mapMessage(msg, s.configurationSet)

	// 映射附件可能耗时；调用 Client 前再次检查，避免已取消请求继续发信。
	if err := ctx.Err(); err != nil {
		return mail.Receipt{}, err
	}

	output, err := s.client.SendEmail(ctx, input)
	if err != nil {
		return mail.Receipt{}, mapError(err)
	}
	if output == nil || output.MessageId == nil || *output.MessageId == "" {
		return mail.Receipt{}, &mail.DeliveryError{
			Provider: Provider,
			Code:     "invalid_response",
			Outcome:  mail.SendOutcomeUnknown,
			Err:      errors.New("mail/ses: successful response is missing message id"),
		}
	}

	return mail.Receipt{
		Provider:  Provider,
		MessageID: *output.MessageId,
	}, nil
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
