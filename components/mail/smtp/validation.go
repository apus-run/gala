package smtp

import (
	"fmt"
	"net/textproto"
	"strings"

	"github.com/apus-run/gala/components/mail"
)

const (
	// maxRecipients 是 RFC 5321 §4.5.3.1.8 要求服务器至少支持的
	// RCPT 数量；超出后各服务器行为不一，前置拒绝。
	maxRecipients = 100
	// maxTagPartLength 与 resend/ses 的 tag 规则对齐（SPEC D4）。
	maxTagPartLength = 256
	// tagHeaderPrefix 是 Tags 映射的自定义头命名空间，归适配器所有。
	tagHeaderPrefix = "X-Tag-"
)

// smtpReservedHeaders 补充核心公共模型之外，本适配器或传输链路
// 拥有的 Header。核心已拒绝 From/To/Cc/Bcc/Reply-To/Subject/
// Content-Type/MIME-Version；这里的 Key 同为 CanonicalMIMEHeaderKey 形式。
var smtpReservedHeaders = map[string]struct{}{
	"Date":                      {},
	"Message-Id":                {},
	"Return-Path":               {},
	"Received":                  {},
	"Content-Transfer-Encoding": {},
	"Content-Disposition":       {},
}

// validateProviderMessage 检查 SMTP 通道的稳定输入约束。调用方必须先
// 执行 mail.Message.Validate；这里的错误同样属于未调用 Provider 的
// mail.ErrInvalidMessage。
func validateProviderMessage(msg mail.Message) error {
	if err := validateRecipientCount(msg); err != nil {
		return err
	}
	if err := validateSMTPAddresses(msg); err != nil {
		return err
	}
	if err := validateSMTPHeaders(msg.Headers); err != nil {
		return err
	}
	if err := validateSMTPTags(msg.Tags); err != nil {
		return err
	}
	return validateSMTPAttachments(msg.Attachments)
}

func validateRecipientCount(msg mail.Message) error {
	count := 0
	for _, addresses := range [][]mail.Address{msg.To, msg.Cc, msg.Bcc} {
		if len(addresses) > maxRecipients-count {
			return fmt.Errorf("%w: smtp supports at most %d recipients", mail.ErrInvalidMessage, maxRecipients)
		}
		count += len(addresses)
	}
	return nil
}

func validateSMTPAddresses(msg mail.Message) error {
	if err := validateSMTPAddress("from", msg.From); err != nil {
		return err
	}
	for _, group := range []struct {
		field     string
		addresses []mail.Address
	}{
		{field: "to", addresses: msg.To},
		{field: "cc", addresses: msg.Cc},
		{field: "bcc", addresses: msg.Bcc},
	} {
		for _, address := range group.addresses {
			if err := validateSMTPAddress(group.field, address); err != nil {
				return err
			}
		}
	}
	if msg.ReplyTo != nil {
		return validateSMTPAddress("reply-to", *msg.ReplyTo)
	}
	return nil
}

// validateSMTPAddress 要求邮箱为 7-bit ASCII（SPEC D8）：不依赖服务器
// SMTPUTF8 能力，国际化域名由应用预先 punycode 编码。显示名不受限，
// go-mail 会按 RFC 2047 编码。
func validateSMTPAddress(field string, address mail.Address) error {
	if !isASCII(address.Email) {
		return fmt.Errorf("%w: smtp %s email must be 7-bit ASCII; encode international domains with punycode",
			mail.ErrInvalidMessage, field)
	}
	return nil
}

func validateSMTPHeaders(headers map[string]string) error {
	for name, value := range headers {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if strings.HasPrefix(canonical, tagHeaderPrefix) {
			return fmt.Errorf("%w: smtp header %q uses the reserved %s namespace; set Tags instead",
				mail.ErrInvalidMessage, name, tagHeaderPrefix)
		}
		if _, reserved := smtpReservedHeaders[canonical]; reserved {
			return fmt.Errorf("%w: smtp header %q is reserved", mail.ErrInvalidMessage, name)
		}
		if !isPrintableASCIIHeaderValue(value) {
			return fmt.Errorf("%w: smtp header %q value must be printable ASCII", mail.ErrInvalidMessage, name)
		}
	}
	return nil
}

func validateSMTPTags(tags map[string]string) error {
	for name, value := range tags {
		if !validTagPart(name) {
			return fmt.Errorf("%w: invalid smtp tag name", mail.ErrInvalidMessage)
		}
		if !validTagPart(value) {
			return fmt.Errorf("%w: smtp tag %q has an invalid value", mail.ErrInvalidMessage, name)
		}
	}
	return nil
}

func validateSMTPAttachments(attachments []mail.Attachment) error {
	for _, attachment := range attachments {
		if attachment.ContentID == "" {
			continue
		}
		if !validContentID(attachment.ContentID) {
			return fmt.Errorf("%w: smtp attachment %q content id must be printable ASCII without spaces or angle brackets",
				mail.ErrInvalidMessage, attachment.Filename)
		}
	}
	return nil
}

func validateMessageID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: smtp message id must not be empty", mail.ErrInvalidMessage)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c < 0x21 || c > 0x7e || c == '<' || c == '>' || c == ',' || c == ';' {
			return fmt.Errorf("%w: smtp message id contains invalid characters", mail.ErrInvalidMessage)
		}
	}
	return nil
}

// validTagPart 与 resend/ses 的 tag 字符集一致（SPEC D4），由构造
// 排除 Header 注入与非 token 字符。
func validTagPart(value string) bool {
	if value == "" || len(value) > maxTagPartLength {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func validContentID(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x21 || c > 0x7e || c == '<' || c == '>' {
			return false
		}
	}
	return true
}

func isASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > 0x7f {
			return false
		}
	}
	return true
}

// isPrintableASCIIHeaderValue 在核心校验（拒绝 C0/DEL）之上进一步
// 限定 SMTP 自定义头值为可打印 ASCII、空格与 Tab；非 ASCII 文本
// 应放在 Subject 或正文中，由 go-mail 负责编码。
func isPrintableASCIIHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\t' {
			continue
		}
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
