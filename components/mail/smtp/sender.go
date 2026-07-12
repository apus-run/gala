package smtp

import (
	"context"
	"errors"

	gomail "github.com/wneessen/go-mail"
	gosmtp "github.com/wneessen/go-mail/smtp"

	"github.com/apus-run/gala/components/mail"
)

// Provider 是本适配器写入 Receipt 与 DeliveryError 的标识。
const Provider = "smtp"

// Dialer 为每次 Send 建立一个 SMTP 会话。拨号、发送、关闭三个阶段
// 的失败由此可区分（而非单一 DialAndSend），使 Outcome 分类精确
// （SPEC D1），并让发送成功后的连接关闭失败不再误报为投递失败
// （SPEC D6）。生产环境用 NewDialer 包装官方 *gomail.Client。
type Dialer interface {
	Dial(ctx context.Context) (Session, error)
}

// Session 是一条已建立的 SMTP 连接，用完必须 Close。
type Session interface {
	Send(messages ...*gomail.Msg) error
	Close() error
}

// ClientDialer 用官方 go-mail Client 实现 Dialer。
// go-mail 每次拨号建立独立连接，同一 ClientDialer 可并发使用。
type ClientDialer struct {
	client *gomail.Client
}

var _ Dialer = (*ClientDialer)(nil)

// NewDialer 包装官方 go-mail Client（主机、端口、认证、TLS 策略、
// 超时全部在其上配置）。
func NewDialer(client *gomail.Client) *ClientDialer {
	return &ClientDialer{client: client}
}

// Dial 实现 Dialer。
func (d *ClientDialer) Dial(ctx context.Context) (Session, error) {
	if d.client == nil {
		return nil, errors.New("mail/smtp: nil go-mail client")
	}
	smtpClient, err := d.client.DialToSMTPClientWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return &clientSession{client: d.client, smtp: smtpClient}, nil
}

type clientSession struct {
	client *gomail.Client
	smtp   *gosmtp.Client
}

func (s *clientSession) Send(messages ...*gomail.Msg) error {
	return s.client.SendWithSMTPClient(s.smtp, messages...)
}

func (s *clientSession) Close() error {
	return s.client.CloseWithSMTPClient(s.smtp)
}

// Sender 通过 SMTP 发送邮件。构造后不可变，可并发使用；
// 每次 Send 建立并关闭独立连接。
type Sender struct {
	dialer            Dialer
	maxMessageBytes   int64
	generateMessageID func() string
}

var _ mail.Sender = (*Sender)(nil)

// New 用注入的 Dialer 构造 Sender。适配器不读取环境变量，
// 也不提供主机/凭据便捷构造器。
func New(dialer Dialer, options ...Option) (*Sender, error) {
	if dialer == nil {
		return nil, errors.New("mail/smtp: nil dialer")
	}
	s := &Sender{dialer: dialer, maxMessageBytes: DefaultMaxMessageBytes}
	for _, option := range options {
		option(s)
	}
	if s.maxMessageBytes <= 0 {
		return nil, errors.New("mail/smtp: max message bytes must be positive")
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

	gmsg, messageID, err := buildMessage(msg, s.generateMessageID)
	if err != nil {
		return mail.Receipt{}, err
	}

	// 构造附件可能耗时；建连前再次检查，避免已取消请求继续发信。
	if err := ctx.Err(); err != nil {
		return mail.Receipt{}, err
	}

	session, err := s.dialer.Dial(ctx)
	if err != nil {
		// 拨号期间不可能发生邮件事务，Outcome 一律 NotAccepted。
		return mail.Receipt{}, mapDialError(err)
	}
	// 连接清理失败不是投递失败（SPEC D6）：发送成功后的 Close 错误吞掉，
	// 发送失败时以发送错误为准，避免调用方对已接受的邮件重试。
	defer func() { _ = session.Close() }()

	if err := session.Send(gmsg); err != nil {
		return mail.Receipt{}, mapSendError(err)
	}

	return mail.Receipt{
		Provider:  Provider,
		MessageID: messageID,
	}, nil
}
