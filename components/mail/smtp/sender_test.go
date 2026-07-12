package smtp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	gomail "github.com/wneessen/go-mail"

	"github.com/apus-run/gala/components/mail"
	"github.com/apus-run/gala/components/mail/mailtest"
)

type fakeDialer struct {
	spec     mailtest.ClientSpec
	dialErr  error
	closeErr error

	sent   []*gomail.Msg
	dialed bool
	closed bool
}

func (f *fakeDialer) Dial(ctx context.Context) (Session, error) {
	if f.spec.Called != nil {
		*f.spec.Called = true
	}
	f.dialed = true
	if f.dialErr != nil {
		return nil, f.dialErr
	}
	return &fakeSession{dialer: f}, nil
}

type fakeSession struct {
	dialer *fakeDialer
}

func (s *fakeSession) Send(messages ...*gomail.Msg) error {
	s.dialer.sent = append(s.dialer.sent, messages...)
	if s.dialer.spec.Err != nil {
		return s.dialer.spec.Err
	}
	return nil
}

func (s *fakeSession) Close() error {
	s.dialer.closed = true
	return s.dialer.closeErr
}

// newTestSender 遵循 SPEC §9.1 的工厂规则：spec.MessageID 非空时注入
// 确定性生成器，为空时用默认生成器（避免空 ID 在建连前被拒）。
func newTestSender(t *testing.T, spec mailtest.ClientSpec, options ...Option) (*Sender, *fakeDialer) {
	t.Helper()
	client := &fakeDialer{spec: spec}
	if spec.MessageID != "" {
		id := spec.MessageID
		options = append([]Option{WithMessageIDGenerator(func() string { return id })}, options...)
	}
	sender, err := New(client, options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return sender, client
}

func renderLastMessage(t *testing.T, client *fakeDialer) string {
	t.Helper()
	if len(client.sent) != 1 {
		t.Fatalf("want exactly 1 sent message, got %d", len(client.sent))
	}
	var buf bytes.Buffer
	if _, err := client.sent[0].WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return buf.String()
}

func TestSenderContract(t *testing.T) {
	mailtest.RunSenderContract(t, mailtest.SenderFactory{
		Provider: Provider,
		New: func(t *testing.T, spec mailtest.ClientSpec) mail.Sender {
			sender, _ := newTestSender(t, spec)
			return sender
		},
	})
}

func TestNewValidation(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) must fail")
	}
	if _, err := New(&fakeDialer{}, WithMaxMessageBytes(0)); err == nil {
		t.Fatal("non-positive budget must fail")
	}
	if _, err := New(&fakeDialer{}, WithMessageIDGenerator(nil)); err != nil {
		t.Fatalf("nil generator restores the default: %v", err)
	}
}

func TestRenderedMessage(t *testing.T) {
	sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "msg-render@example.org"})

	msg := mailtest.ValidMessage()
	receipt, err := sender.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	rendered := renderLastMessage(t, client)

	for name, want := range map[string]string{
		"from":               "<hello@example.com>",
		"to":                 "<to@example.com>",
		"cc":                 "<cc@example.com>",
		"reply-to":           "<reply@example.com>",
		"message id header":  "Message-ID: <msg-render@example.org>",
		"custom header":      "X-Entity-Ref-ID: abc",
		"tag header":         "X-Tag-kind: contract",
		"date header":        "Date: ",
		"alternative":        "multipart/alternative",
		"text part":          "text/plain",
		"html part":          "text/html",
		"attachment name":    `a.txt`,
		"inline content id":  "Content-Id: <logo>",
		"inline disposition": "Content-Disposition: inline",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered message missing %s (%q)", name, want)
		}
	}

	// Bcc 只进信封，不得出现在渲染头部。
	if strings.Contains(rendered, "bcc@example.com") || strings.Contains(rendered, "Bcc:") {
		t.Error("bcc must not appear in the rendered message")
	}
	if receipt.MessageID != "msg-render@example.org" {
		t.Errorf("receipt message id = %q", receipt.MessageID)
	}

	// 深拷贝：go-mail 已缓冲附件，调用方随后修改不影响渲染结果。
	msg.Attachments[0].Content[0] = 'X'
	var again bytes.Buffer
	if _, err := client.sent[0].WriteTo(&again); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if again.String() != rendered {
		t.Error("rendered message must not alias caller attachment bytes")
	}
}

func TestBodyMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate      func(*mail.Message)
		wants, nots []string
	}{
		"text only": {
			mutate: func(m *mail.Message) { m.HTML = ""; m.Attachments = nil },
			wants:  []string{"text/plain"},
			nots:   []string{"text/html", "multipart/alternative"},
		},
		"html only": {
			mutate: func(m *mail.Message) { m.Text = ""; m.Attachments = nil },
			wants:  []string{"text/html"},
			nots:   []string{"text/plain", "multipart/alternative"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
			msg := mailtest.ValidMessage()
			tc.mutate(&msg)
			if _, err := sender.Send(context.Background(), msg); err != nil {
				t.Fatalf("Send: %v", err)
			}
			rendered := renderLastMessage(t, client)
			for _, want := range tc.wants {
				if !strings.Contains(rendered, want) {
					t.Errorf("missing %q", want)
				}
			}
			for _, not := range tc.nots {
				if strings.Contains(rendered, not) {
					t.Errorf("unexpected %q", not)
				}
			}
		})
	}
}

func TestMessageIDGeneration(t *testing.T) {
	t.Run("default generator produces a usable id", func(t *testing.T) {
		sender, client := newTestSender(t, mailtest.ClientSpec{})
		receipt, err := sender.Send(context.Background(), mailtest.ValidMessage())
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if receipt.MessageID == "" || strings.ContainsAny(receipt.MessageID, "<>") {
			t.Fatalf("unexpected message id %q", receipt.MessageID)
		}
		if !strings.Contains(renderLastMessage(t, client), "Message-ID: <"+receipt.MessageID+">") {
			t.Fatal("receipt message id must match the rendered header")
		}
	})

	for name, id := range map[string]string{
		"empty":          "",
		"angle brackets": "a<b>c@example.org",
		"space":          "id with space@example.org",
		"comma":          "a,b@example.org",
		"non-ascii":      "凯拉@example.org",
	} {
		t.Run("invalid generated id "+name, func(t *testing.T) {
			client := &fakeDialer{}
			sender, err := New(client, WithMessageIDGenerator(func() string { return id }))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = sender.Send(context.Background(), mailtest.ValidMessage())
			if !errors.Is(err, mail.ErrInvalidMessage) {
				t.Fatalf("want ErrInvalidMessage, got %v", err)
			}
			if client.dialed {
				t.Fatal("client must not be dialed with an invalid message id")
			}
		})
	}
}

func TestProviderValidation(t *testing.T) {
	tooMany := make([]mail.Address, maxRecipients+1)
	for i := range tooMany {
		tooMany[i] = mail.Address{Email: "to@example.com"}
	}

	tests := []struct {
		name   string
		mutate func(*mail.Message)
	}{
		{"too many recipients", func(m *mail.Message) { m.To = tooMany }},
		{"recipients overflow across fields", func(m *mail.Message) {
			m.To = tooMany[:maxRecipients]
			m.Cc = []mail.Address{{Email: "cc@example.com"}}
		}},
		{"non-ascii from email", func(m *mail.Message) { m.From.Email = "凯拉@example.com" }},
		{"non-ascii to email", func(m *mail.Message) { m.To = []mail.Address{{Email: "tô@example.com"}} }},
		{"non-ascii reply-to email", func(m *mail.Message) { m.ReplyTo = &mail.Address{Email: "rép@example.com"} }},
		{"x-tag namespace header", func(m *mail.Message) { m.Headers = map[string]string{"X-Tag-kind": "x"} }},
		{"x-tag namespace header case insensitive", func(m *mail.Message) { m.Headers = map[string]string{"x-tAg-foo": "x"} }},
		{"reserved header date", func(m *mail.Message) { m.Headers = map[string]string{"date": "x"} }},
		{"reserved header message-id", func(m *mail.Message) { m.Headers = map[string]string{"Message-ID": "x"} }},
		{"reserved header return-path", func(m *mail.Message) { m.Headers = map[string]string{"Return-Path": "x"} }},
		{"reserved header received", func(m *mail.Message) { m.Headers = map[string]string{"Received": "x"} }},
		{"reserved header content-transfer-encoding", func(m *mail.Message) {
			m.Headers = map[string]string{"content-transfer-encoding": "x"}
		}},
		{"reserved header content-disposition", func(m *mail.Message) {
			m.Headers = map[string]string{"Content-Disposition": "x"}
		}},
		{"non-ascii header value", func(m *mail.Message) { m.Headers = map[string]string{"X-A": "值"} }},
		{"empty tag name", func(m *mail.Message) { m.Tags = map[string]string{"": "v"} }},
		{"empty tag value", func(m *mail.Message) { m.Tags = map[string]string{"n": ""} }},
		{"tag name with space", func(m *mail.Message) { m.Tags = map[string]string{"bad name": "v"} }},
		{"tag value non-ascii", func(m *mail.Message) { m.Tags = map[string]string{"n": "值"} }},
		{"tag name too long", func(m *mail.Message) {
			m.Tags = map[string]string{strings.Repeat("n", maxTagPartLength+1): "v"}
		}},
		{"content id with space", func(m *mail.Message) { m.Attachments[1].ContentID = "lo go" }},
		{"content id with angle bracket", func(m *mail.Message) { m.Attachments[1].ContentID = "<logo>" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			sender, client := newTestSender(t, mailtest.ClientSpec{Called: &called})
			msg := mailtest.ValidMessage()
			tt.mutate(&msg)

			_, err := sender.Send(context.Background(), msg)
			if !errors.Is(err, mail.ErrInvalidMessage) {
				t.Fatalf("want ErrInvalidMessage, got %v", err)
			}
			if called || client.dialed {
				t.Fatal("client must not be dialed for a provider-invalid message")
			}
		})
	}

	t.Run("provider boundaries accepted", func(t *testing.T) {
		to := make([]mail.Address, maxRecipients)
		for i := range to {
			to[i] = mail.Address{Email: "to@example.com"}
		}
		msg := mailtest.ValidMessage()
		msg.To = to
		msg.Cc = nil
		msg.Bcc = nil
		msg.Headers = map[string]string{"X-A": "tab\tand printable"}
		msg.Tags = map[string]string{
			strings.Repeat("n", maxTagPartLength): strings.Repeat("v", maxTagPartLength),
		}

		sender, _ := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		if _, err := sender.Send(context.Background(), msg); err != nil {
			t.Fatalf("boundary-valid message: %v", err)
		}
	})
}

func TestMessageBudget(t *testing.T) {
	called := false
	sender, client := newTestSender(t, mailtest.ClientSpec{Called: &called}, WithMaxMessageBytes(64))

	msg := mail.Message{
		From: mail.Address{Email: "a@example.com"},
		To:   []mail.Address{{Email: "b@example.com"}},
		Text: strings.Repeat("x", 128),
	}
	_, err := sender.Send(context.Background(), msg)
	if !errors.Is(err, mail.ErrInvalidMessage) {
		t.Fatalf("want ErrInvalidMessage, got %v", err)
	}
	if called || client.dialed {
		t.Fatal("client must not be dialed when over budget")
	}

	msg.Text = "small"
	if _, err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("within budget: %v", err)
	}
}

func assertClassification(t *testing.T, err error, code string, transient bool, outcome mail.SendOutcome) {
	t.Helper()
	var deliveryErr *mail.DeliveryError
	if !errors.As(err, &deliveryErr) {
		t.Fatalf("want DeliveryError, got %T: %v", err, err)
	}
	if deliveryErr.Provider != Provider {
		t.Fatalf("provider = %q", deliveryErr.Provider)
	}
	if deliveryErr.Code != code {
		t.Fatalf("code = %q, want %q", deliveryErr.Code, code)
	}
	if deliveryErr.IsTransient != transient {
		t.Fatalf("IsTransient = %v, want %v", deliveryErr.IsTransient, transient)
	}
	if deliveryErr.Outcome != outcome {
		t.Fatalf("Outcome = %v, want %v", deliveryErr.Outcome, outcome)
	}
}

func TestDialErrorClassification(t *testing.T) {
	dial := func(t *testing.T, dialErr error) error {
		t.Helper()
		sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		client.dialErr = dialErr
		_, err := sender.Send(context.Background(), mailtest.ValidMessage())
		if err == nil {
			t.Fatal("want error")
		}
		if len(client.sent) != 0 {
			t.Fatal("send must not run after a dial failure")
		}
		return err
	}

	t.Run("wrapped cancel", func(t *testing.T) {
		err := dial(t, fmt.Errorf("dial failed: %w", context.Canceled))
		assertClassification(t, err, "canceled", false, mail.SendOutcomeNotAccepted)
		if !errors.Is(err, context.Canceled) {
			t.Fatal("errors.Is must reach context.Canceled")
		}
	})

	t.Run("wrapped deadline", func(t *testing.T) {
		err := dial(t, fmt.Errorf("dial failed: %w", context.DeadlineExceeded))
		assertClassification(t, err, "deadline_exceeded", true, mail.SendOutcomeNotAccepted)
	})

	t.Run("timeout", func(t *testing.T) {
		err := dial(t, &timeoutError{})
		assertClassification(t, err, "dial", true, mail.SendOutcomeNotAccepted)
	})

	t.Run("connection refused", func(t *testing.T) {
		refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		err := dial(t, fmt.Errorf("dial failed: %w", refused))
		assertClassification(t, err, "dial", true, mail.SendOutcomeNotAccepted)
	})

	t.Run("other dial failure is permanent", func(t *testing.T) {
		err := dial(t, errors.New("tls: handshake failure"))
		assertClassification(t, err, "dial", false, mail.SendOutcomeNotAccepted)
		if mail.MayHaveBeenAccepted(err) {
			t.Fatal("dial failures can never have been accepted")
		}
	})
}

func TestSendErrorClassification(t *testing.T) {
	send := func(t *testing.T, sendErr error) error {
		t.Helper()
		sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1", Err: sendErr})
		_, err := sender.Send(context.Background(), mailtest.ValidMessage())
		if err == nil {
			t.Fatal("want error")
		}
		if !client.closed {
			t.Fatal("connection must be closed after a send failure")
		}
		return err
	}

	// SendError 字面量的 isTemp 不可导出（恒 false）；瞬态真值分支由
	// 集成测试覆盖（SPEC A2）。
	notAccepted := map[gomail.SendErrReason]string{
		gomail.ErrGetSender:    "get_sender",
		gomail.ErrGetRcpts:     "get_rcpts",
		gomail.ErrNoUnencoded:  "no_unencoded",
		gomail.ErrConnCheck:    "conn_check",
		gomail.ErrSMTPMailFrom: "mail_from",
		gomail.ErrSMTPRcptTo:   "rcpt_to",
		gomail.ErrSMTPData:     "data",
		gomail.ErrWriteContent: "write_content",
	}
	for reason, code := range notAccepted {
		t.Run("not accepted "+code, func(t *testing.T) {
			err := send(t, &gomail.SendError{Reason: reason})
			assertClassification(t, err, code, false, mail.SendOutcomeNotAccepted)
			if mail.MayHaveBeenAccepted(err) {
				t.Fatal("pre-termination rejection must not count as possibly accepted")
			}
		})
	}

	unknown := map[gomail.SendErrReason]string{
		gomail.ErrSMTPDataClose: "data_close",
		gomail.ErrSMTPReset:     "reset",
		gomail.ErrAmbiguous:     "ambiguous",
	}
	for reason, code := range unknown {
		t.Run("unknown outcome "+code, func(t *testing.T) {
			err := send(t, &gomail.SendError{Reason: reason})
			assertClassification(t, err, code, false, mail.SendOutcomeUnknown)
			if !mail.MayHaveBeenAccepted(err) {
				t.Fatal("post-termination failures may have been accepted")
			}
		})
	}

	t.Run("unrecognised reason stays unknown", func(t *testing.T) {
		err := send(t, &gomail.SendError{Reason: gomail.SendErrReason(255)})
		assertClassification(t, err, "ambiguous", false, mail.SendOutcomeUnknown)
	})

	t.Run("network timeout", func(t *testing.T) {
		err := send(t, &timeoutError{})
		assertClassification(t, err, "timeout", true, mail.SendOutcomeUnknown)
	})

	t.Run("unknown error is conservative", func(t *testing.T) {
		sentinel := errors.New("boom")
		err := send(t, sentinel)
		assertClassification(t, err, "", false, mail.SendOutcomeUnknown)
		if !errors.Is(err, sentinel) {
			t.Fatal("original error must stay reachable")
		}
	})
}

func TestCloseHandling(t *testing.T) {
	t.Run("close failure after successful send is swallowed", func(t *testing.T) {
		sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		client.closeErr = errors.New("quit failed")

		receipt, err := sender.Send(context.Background(), mailtest.ValidMessage())
		if err != nil {
			t.Fatalf("close failure must not fail an accepted send: %v", err)
		}
		if receipt.MessageID != "id-1" || !client.closed {
			t.Fatalf("unexpected state: receipt=%+v closed=%v", receipt, client.closed)
		}
	})

	t.Run("send failure wins over close failure", func(t *testing.T) {
		sentinel := errors.New("rcpt rejected")
		sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1", Err: sentinel})
		client.closeErr = errors.New("quit failed")

		_, err := sender.Send(context.Background(), mailtest.ValidMessage())
		if !errors.Is(err, sentinel) {
			t.Fatalf("want the send error, got %v", err)
		}
		if !client.closed {
			t.Fatal("connection must still be closed")
		}
	})
}

type timeoutError struct{}

func (*timeoutError) Error() string   { return "i/o timeout" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }
