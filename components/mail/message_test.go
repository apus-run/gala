package mail

import (
	"errors"
	"strings"
	"testing"
)

func validMessage() Message {
	return Message{
		From:    Address{Name: "Gala", Email: "hello@example.com"},
		To:      []Address{{Email: "to@example.com"}},
		Subject: "hi",
		Text:    "body",
	}
}

func TestAddressString(t *testing.T) {
	tests := []struct {
		name string
		addr Address
		want string
	}{
		{"email only", Address{Email: "hello@example.com"}, "<hello@example.com>"},
		{"ascii name", Address{Name: "Gala", Email: "hello@example.com"}, `"Gala" <hello@example.com>`},
		{"unicode name rfc2047", Address{Name: "凯拉", Email: "hello@example.com"}, "=?utf-8?q?=E5=87=AF=E6=8B=89?= <hello@example.com>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addr.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Message)
		wantErr bool
	}{
		{"valid minimal", func(m *Message) {}, false},
		{"valid html only", func(m *Message) { m.Text = ""; m.HTML = "<b>hi</b>" }, false},
		{"valid attachment only", func(m *Message) {
			m.Text = ""
			m.Attachments = []Attachment{{Filename: "a.txt", Content: []byte("x")}}
		}, false},
		{"valid inline attachment", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "l.png", ContentID: "logo", Content: []byte{1}}}
		}, false},
		{"valid custom header", func(m *Message) { m.Headers = map[string]string{"X-Ref": "1"} }, false},
		{"valid header name ftext boundaries", func(m *Message) { m.Headers = map[string]string{"!X~": "1"} }, false},
		{"valid header value with tab", func(m *Message) { m.Headers = map[string]string{"X-Ref": "a\tb"} }, false},
		{"valid unicode header value", func(m *Message) { m.Headers = map[string]string{"X-Label": "凯拉"} }, false},
		{"valid unicode display name", func(m *Message) { m.From.Name = "凯拉 · Gala" }, false},
		{"valid cc only recipient", func(m *Message) { m.To = nil; m.Cc = []Address{{Email: "cc@example.com"}} }, false},
		{"valid bcc only recipient", func(m *Message) { m.To = nil; m.Bcc = []Address{{Email: "bcc@example.com"}} }, false},
		{"valid reply-to", func(m *Message) { m.ReplyTo = &Address{Email: "reply@example.com"} }, false},

		{"empty from", func(m *Message) { m.From = Address{} }, true},
		{"from without at sign", func(m *Message) { m.From.Email = "not-an-email" }, true},
		{"from missing domain", func(m *Message) { m.From.Email = "a@" }, true},
		{"from name with newline", func(m *Message) { m.From.Name = "a\nb" }, true},
		{"from email with crlf", func(m *Message) { m.From.Email = "a@example.com\r\nBcc: x@y.z" }, true},
		{"no recipients", func(m *Message) { m.To = nil }, true},
		{"invalid to", func(m *Message) { m.To = []Address{{Email: "bad"}} }, true},
		{"invalid cc", func(m *Message) { m.Cc = []Address{{Email: "bad"}} }, true},
		{"invalid bcc", func(m *Message) { m.Bcc = []Address{{Email: "bad"}} }, true},
		{"invalid reply-to", func(m *Message) { m.ReplyTo = &Address{Email: "bad"} }, true},
		{"subject with crlf", func(m *Message) { m.Subject = "hello\r\nBcc: victim@example.com" }, true},
		{"subject with nul", func(m *Message) { m.Subject = "hello\x00world" }, true},
		{"no content", func(m *Message) { m.Text = "" }, true},

		{"header name with lf", func(m *Message) { m.Headers = map[string]string{"X-A\nB": "v"} }, true},
		{"header name with space", func(m *Message) { m.Headers = map[string]string{"X A": "v"} }, true},
		{"header name with colon", func(m *Message) { m.Headers = map[string]string{"X:A": "v"} }, true},
		{"header name with del", func(m *Message) { m.Headers = map[string]string{"X\x7fA": "v"} }, true},
		{"non-ascii header name", func(m *Message) { m.Headers = map[string]string{"X-标签": "v"} }, true},
		{"header value with cr", func(m *Message) { m.Headers = map[string]string{"X-A": "v\rx"} }, true},
		{"header value with nul", func(m *Message) { m.Headers = map[string]string{"X-A": "v\x00x"} }, true},
		{"header value with unit separator", func(m *Message) { m.Headers = map[string]string{"X-A": "v\x1fx"} }, true},
		{"header value with del", func(m *Message) { m.Headers = map[string]string{"X-A": "v\x7fx"} }, true},
		{"empty header name", func(m *Message) { m.Headers = map[string]string{"": "v"} }, true},
		{"reserved header subject", func(m *Message) { m.Headers = map[string]string{"Subject": "v"} }, true},
		{"reserved header lowercase", func(m *Message) { m.Headers = map[string]string{"subject": "v"} }, true},
		{"reserved header uppercase", func(m *Message) { m.Headers = map[string]string{"SUBJECT": "v"} }, true},
		{"reserved header bcc mixed case", func(m *Message) { m.Headers = map[string]string{"bCC": "v"} }, true},
		{"reserved header content-type", func(m *Message) { m.Headers = map[string]string{"content-type": "v"} }, true},
		{"reserved header mime-version", func(m *Message) { m.Headers = map[string]string{"MIME-Version": "v"} }, true},
		{"reserved header reply-to", func(m *Message) { m.Headers = map[string]string{"reply-to": "v"} }, true},

		{"attachment empty filename", func(m *Message) {
			m.Attachments = []Attachment{{Content: []byte{1}}}
		}, true},
		{"attachment empty content", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a.txt"}}
		}, true},
		{"attachment filename with crlf", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a.txt\r\nX-Bad: 1", Content: []byte{1}}}
		}, true},
		{"attachment filename with nul", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a\x00.txt", Content: []byte{1}}}
		}, true},
		{"attachment content type with crlf", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a.txt", ContentType: "text/plain\r\nX-Bad: 1", Content: []byte{1}}}
		}, true},
		{"attachment content id with control char", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a.txt", ContentID: "x\x01y", Content: []byte{1}}}
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := validMessage()
			tt.mutate(&msg)
			err := msg.Validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidMessage) {
					t.Fatalf("want ErrInvalidMessage, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateErrorMentionsField(t *testing.T) {
	msg := validMessage()
	msg.To = []Address{{Email: "bad"}}
	err := msg.Validate()
	if err == nil || !strings.Contains(err.Error(), "to") {
		t.Fatalf("error should mention the failing field, got %v", err)
	}
}
