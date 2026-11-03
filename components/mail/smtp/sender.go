package smtp

import (
	"context"
	"errors"

	gomail "github.com/wneessen/go-mail"
	gosmtp "github.com/wneessen/go-mail/smtp"

	"github.com/apus-run/gala/components/mail"
)

// Provider identifies this adapter in Receipt and DeliveryError values
// | Provider 是本适配器写入 Receipt 与 DeliveryError 的标识。
const Provider = "smtp"

// Dialer opens one SMTP session per Send. Keeping dial, send and close as
// distinct phases (instead of a single DialAndSend) makes the Outcome
// classification precise (SPEC D1) and stops a connection-close failure after
// a successful send from being misreported as a delivery failure (SPEC D6).
// Production code wraps the official *gomail.Client with NewDialer.
// Dialer 为每次 Send 建立一个 SMTP 会话。拨号、发送、关闭三个阶段
// 的失败由此可区分（而非单一 DialAndSend），使 Outcome 分类精确
// （SPEC D1），并让发送成功后的连接关闭失败不再误报为投递失败
// （SPEC D6）。生产环境用 NewDialer 包装官方 *gomail.Client。
type Dialer interface {
	Dial(ctx context.Context) (Session, error)
}

// Session is one established SMTP connection and must be Closed after use
// | Session 是一条已建立的 SMTP 连接，用完必须 Close。
type Session interface {
	// Send sends the messages on this connection | 在该连接上发送邮件消息.
	Send(messages ...*gomail.Msg) error
	// Close releases the connection | 关闭并释放连接.
	Close() error
}

// ClientDialer implements Dialer on the official go-mail Client. go-mail opens
// a dedicated connection per dial, so one ClientDialer is safe for concurrent
// use.
// ClientDialer 用官方 go-mail Client 实现 Dialer。
// go-mail 每次拨号建立独立连接，同一 ClientDialer 可并发使用。
type ClientDialer struct {
	client *gomail.Client
}

var _ Dialer = (*ClientDialer)(nil)

// NewDialer wraps the official go-mail Client (host, port, auth, TLS policy
// and timeouts are all configured on it).
// NewDialer 包装官方 go-mail Client（主机、端口、认证、TLS 策略、
// 超时全部在其上配置）。
func NewDialer(client *gomail.Client) *ClientDialer {
	return &ClientDialer{client: client}
}

// Dial implements Dialer | Dial 实现 Dialer。
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

// Send implements Session | Send 实现 Session.
func (s *clientSession) Send(messages ...*gomail.Msg) error {
	return s.client.SendWithSMTPClient(s.smtp, messages...)
}

// Close implements Session | Close 实现 Session.
func (s *clientSession) Close() error {
	return s.client.CloseWithSMTPClient(s.smtp)
}

// Sender sends mail over SMTP. It is immutable after construction and safe
// for concurrent use; every Send opens and closes a dedicated connection.
// Sender 通过 SMTP 发送邮件。构造后不可变，可并发使用；
// 每次 Send 建立并关闭独立连接。
type Sender struct {
	dialer            Dialer
	maxMessageBytes   int64
	generateMessageID func() string
}

var _ mail.Sender = (*Sender)(nil)

// New builds a Sender from the injected Dialer. The adapter reads no
// environment variables and offers no host/credential convenience constructor.
// New 用注入的 Dialer 构造 Sender。适配器不读取环境变量，
// 也不提供主机/凭据便捷构造器。
func New(dialer Dialer, options ...Option) (*Sender, error) {
	if dialer == nil {
		return nil, errors.New("mail/smtp: nil dialer")
	}
	s := &Sender{dialer: dialer, maxMessageBytes: DefaultMaxMessageBytes}
	for _, option := range options {
		option.apply(s)
	}
	if s.maxMessageBytes <= 0 {
		return nil, errors.New("mail/smtp: max message bytes must be positive")
	}
	return s, nil
}

// Send implements mail.Sender | Send 实现 mail.Sender。
func (s *Sender) Send(ctx context.Context, msg mail.Message) (mail.Receipt, error) {
	// Contract (design doc §19.2): with an already-canceled Context, do not
	// call the Client and return the error as-is.
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

	// Building attachments may take time; re-check before connecting so an
	// already-canceled request does not still send the mail.
	// 构造附件可能耗时；建连前再次检查，避免已取消请求继续发信。
	if err := ctx.Err(); err != nil {
		return mail.Receipt{}, err
	}

	session, err := s.dialer.Dial(ctx)
	if err != nil {
		// No mail transaction can happen during dialing, so the Outcome is
		// always NotAccepted.
		// 拨号期间不可能发生邮件事务，Outcome 一律 NotAccepted。
		return mail.Receipt{}, mapDialError(err)
	}
	// A connection-cleanup failure is not a delivery failure (SPEC D6): after
	// a successful send the Close error is swallowed; on send failure the send
	// error wins, so the caller never retries mail the server already accepted.
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
