package mail

import (
	"fmt"
	netmail "net/mail"
	"net/textproto"
)

// reservedHeaders 是不允许通过自定义 Headers 重写的结构字段，
// Key 为 textproto.CanonicalMIMEHeaderKey 归一化后的形式。
var reservedHeaders = map[string]struct{}{
	"From":         {},
	"To":           {},
	"Cc":           {},
	"Bcc":          {},
	"Reply-To":     {},
	"Subject":      {},
	"Content-Type": {},
	"Mime-Version": {},
}

// Validate 检查跨 Provider 稳定成立的公共规则。
// Provider 特有约束（消息预算、Header 数量、Tag 字符集等）由各适配器
// 在公共校验之后、调用 Client 之前检查。校验失败的错误满足
// errors.Is(err, ErrInvalidMessage)。
func (m Message) Validate() error {
	if err := validateAddress("from", m.From); err != nil {
		return err
	}

	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		return fmt.Errorf("%w: at least one recipient in to, cc or bcc", ErrInvalidMessage)
	}
	for field, addrs := range map[string][]Address{"to": m.To, "cc": m.Cc, "bcc": m.Bcc} {
		for _, addr := range addrs {
			if err := validateAddress(field, addr); err != nil {
				return err
			}
		}
	}
	if m.ReplyTo != nil {
		if err := validateAddress("reply-to", *m.ReplyTo); err != nil {
			return err
		}
	}
	if !validHeaderValue(m.Subject) {
		return fmt.Errorf("%w: subject contains invalid control characters", ErrInvalidMessage)
	}

	if m.Text == "" && m.HTML == "" && len(m.Attachments) == 0 {
		return fmt.Errorf("%w: one of text, html or attachments is required", ErrInvalidMessage)
	}

	for name, value := range m.Headers {
		if name == "" {
			return fmt.Errorf("%w: empty header name", ErrInvalidMessage)
		}
		if !validHeaderName(name) {
			return fmt.Errorf("%w: invalid header name %q", ErrInvalidMessage, name)
		}
		if !validHeaderValue(value) {
			return fmt.Errorf("%w: header %q contains invalid control characters", ErrInvalidMessage, name)
		}
		if _, reserved := reservedHeaders[textproto.CanonicalMIMEHeaderKey(name)]; reserved {
			return fmt.Errorf("%w: header %q overrides a structural field", ErrInvalidMessage, name)
		}
	}

	for i, a := range m.Attachments {
		if a.Filename == "" {
			return fmt.Errorf("%w: attachment %d: empty filename", ErrInvalidMessage, i)
		}
		if !validHeaderValue(a.Filename) {
			return fmt.Errorf("%w: attachment %d: filename contains invalid control characters", ErrInvalidMessage, i)
		}
		if !validHeaderValue(a.ContentType) {
			return fmt.Errorf("%w: attachment %q: content type contains invalid control characters", ErrInvalidMessage, a.Filename)
		}
		if len(a.Content) == 0 {
			return fmt.Errorf("%w: attachment %q: empty content", ErrInvalidMessage, a.Filename)
		}
		if containsControl(a.ContentID) {
			return fmt.Errorf("%w: attachment %q: content id contains control characters", ErrInvalidMessage, a.Filename)
		}
	}

	return nil
}

// validHeaderName implements RFC 5322 field-name/ftext: printable ASCII except
// colon. Iterate by byte so non-ASCII names are rejected rather than partially
// accepted as UTF-8.
func validHeaderName(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] < 33 || name[i] > 126 || name[i] == ':' {
			return false
		}
	}
	return name != ""
}

// validHeaderValue rejects C0 controls and DEL while retaining HTAB, printable
// ASCII, and UTF-8 values. CR and LF remain forbidden, so callers cannot fold or
// inject additional fields.
func validHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] == '\t' {
			continue
		}
		if value[i] < 0x20 || value[i] == 0x7f {
			return false
		}
	}
	return true
}

func validateAddress(field string, a Address) error {
	if a.Email == "" {
		return fmt.Errorf("%w: %s: empty email", ErrInvalidMessage, field)
	}
	if containsControl(a.Email) || containsControl(a.Name) {
		return fmt.Errorf("%w: %s: address contains control characters", ErrInvalidMessage, field)
	}
	if _, err := netmail.ParseAddress(a.String()); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidMessage, field, err)
	}
	return nil
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
