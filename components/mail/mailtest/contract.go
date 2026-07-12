package mailtest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/apus-run/gala/components/mail"
)

// ClientSpec 描述契约测试期望适配器底层假 Client 的行为。
// 适配器测试根据该规格构造只实现窄 Client 接口的 Fake。
type ClientSpec struct {
	// MessageID 是假 Client 成功时返回的 Provider 消息标识。
	MessageID string
	// Err 非 nil 时，假 Client 返回该错误。
	Err error
	// Called 非 nil 时，假 Client 每次被调用都置 *Called = true。
	Called *bool
}

// SenderFactory 为某个 Provider 适配器构造受契约测试控制的 Sender。
type SenderFactory struct {
	// Provider 是适配器写入 Receipt.Provider 与 DeliveryError.Provider 的标识。
	Provider string
	// New 返回一个由符合 spec 的假 Client 支撑的 Sender。
	New func(t *testing.T, spec ClientSpec) mail.Sender
}

// ValidMessage 返回一条通过公共校验、覆盖主要字段的消息，
// 供契约测试与适配器测试复用。
func ValidMessage() mail.Message {
	return mail.Message{
		From:    mail.Address{Name: "Gala", Email: "hello@example.com"},
		To:      []mail.Address{{Name: "收件人", Email: "to@example.com"}},
		Cc:      []mail.Address{{Email: "cc@example.com"}},
		Bcc:     []mail.Address{{Email: "bcc@example.com"}},
		ReplyTo: &mail.Address{Email: "reply@example.com"},
		Subject: "契约测试",
		Text:    "text body",
		HTML:    `<strong>html body</strong> <img src="cid:logo">`,
		Headers: map[string]string{"X-Entity-Ref-ID": "abc"},
		Tags:    map[string]string{"kind": "contract"},
		Attachments: []mail.Attachment{
			{Filename: "a.txt", ContentType: "text/plain", Content: []byte("hello")},
			{Filename: "logo.png", ContentType: "image/png", ContentID: "logo", Content: []byte{0x89, 'P', 'N', 'G'}},
		},
	}
}

// RunSenderContract 对一个 Provider 适配器执行共享行为契约。
// 字段映射的正确性（To/Cc/Bcc 不串位、Bcc 不进公开 Header 等）
// 依赖 Provider 请求类型，由各适配器自己的单元测试覆盖。
func RunSenderContract(t *testing.T, factory SenderFactory) {
	t.Helper()

	t.Run("context cancelled before send", func(t *testing.T) {
		called := false
		sender := factory.New(t, ClientSpec{Called: &called})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := sender.Send(ctx, ValidMessage())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		var deliveryErr *mail.DeliveryError
		if errors.As(err, &deliveryErr) {
			t.Fatalf("pre-call context error must not be a DeliveryError: %v", err)
		}
		if mail.MayHaveBeenAccepted(err) {
			t.Fatal("pre-call context error must not count as possibly accepted")
		}
		if called {
			t.Fatal("client must not be called when context is already cancelled")
		}
	})

	t.Run("context deadline exceeded before send", func(t *testing.T) {
		called := false
		sender := factory.New(t, ClientSpec{Called: &called})

		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		_, err := sender.Send(ctx, ValidMessage())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want context.DeadlineExceeded, got %v", err)
		}
		var deliveryErr *mail.DeliveryError
		if errors.As(err, &deliveryErr) {
			t.Fatalf("pre-call context error must not be a DeliveryError: %v", err)
		}
		if mail.MayHaveBeenAccepted(err) {
			t.Fatal("pre-call context error must not count as possibly accepted")
		}
		if called {
			t.Fatal("client must not be called when context deadline has already elapsed")
		}
	})

	t.Run("invalid message rejected before client", func(t *testing.T) {
		called := false
		sender := factory.New(t, ClientSpec{Called: &called})

		_, err := sender.Send(context.Background(), mail.Message{})
		if !errors.Is(err, mail.ErrInvalidMessage) {
			t.Fatalf("want ErrInvalidMessage, got %v", err)
		}
		var deliveryErr *mail.DeliveryError
		if errors.As(err, &deliveryErr) {
			t.Fatalf("pre-call validation error must not be a DeliveryError: %v", err)
		}
		if mail.MayHaveBeenAccepted(err) {
			t.Fatal("pre-call validation error must not count as possibly accepted")
		}
		if called {
			t.Fatal("client must not be called for an invalid message")
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*mail.Message)
	}{
		{
			name: "subject header injection rejected before client",
			mutate: func(msg *mail.Message) {
				msg.Subject = "hello\r\nBcc: victim@example.com"
			},
		},
		{
			name: "attachment metadata injection rejected before client",
			mutate: func(msg *mail.Message) {
				msg.Attachments[0].Filename = "a.txt\r\nX-Bad: 1"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			sender := factory.New(t, ClientSpec{Called: &called})
			msg := ValidMessage()
			tc.mutate(&msg)

			_, err := sender.Send(context.Background(), msg)
			if !errors.Is(err, mail.ErrInvalidMessage) {
				t.Fatalf("want ErrInvalidMessage, got %v", err)
			}
			if called {
				t.Fatal("client must not be called for unsafe mail metadata")
			}
		})
	}

	t.Run("success maps provider and message id", func(t *testing.T) {
		sender := factory.New(t, ClientSpec{MessageID: "msg-123"})

		receipt, err := sender.Send(context.Background(), ValidMessage())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receipt.Provider != factory.Provider {
			t.Fatalf("want provider %q, got %q", factory.Provider, receipt.Provider)
		}
		if receipt.MessageID != "msg-123" {
			t.Fatalf("want message id msg-123, got %q", receipt.MessageID)
		}
	})

	t.Run("wrapped cancellation during call", func(t *testing.T) {
		sender := factory.New(t, ClientSpec{
			Err: fmt.Errorf("transport: %w", context.Canceled),
		})

		_, err := sender.Send(context.Background(), ValidMessage())
		assertDeliveryError(t, err, factory.Provider, false)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("errors.Is(err, context.Canceled) must hold, got %v", err)
		}
		if !mail.MayHaveBeenAccepted(err) {
			t.Fatal("in-call cancellation must stay SendOutcomeUnknown")
		}
	})

	t.Run("wrapped deadline during call", func(t *testing.T) {
		sender := factory.New(t, ClientSpec{
			Err: fmt.Errorf("transport: %w", context.DeadlineExceeded),
		})

		_, err := sender.Send(context.Background(), ValidMessage())
		assertDeliveryError(t, err, factory.Provider, true)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("errors.Is(err, context.DeadlineExceeded) must hold, got %v", err)
		}
		if !mail.MayHaveBeenAccepted(err) {
			t.Fatal("in-call deadline must stay SendOutcomeUnknown")
		}
	})

	t.Run("original sdk error is unwrappable", func(t *testing.T) {
		sentinel := errors.New("sdk boom")
		sender := factory.New(t, ClientSpec{Err: sentinel})

		_, err := sender.Send(context.Background(), ValidMessage())
		var deliveryErr *mail.DeliveryError
		if !errors.As(err, &deliveryErr) {
			t.Fatalf("want *mail.DeliveryError, got %T: %v", err, err)
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("original sdk error must be reachable via errors.Is, got %v", err)
		}
	})

	t.Run("send does not mutate caller message", func(t *testing.T) {
		sender := factory.New(t, ClientSpec{MessageID: "msg-immutable"})

		msg := ValidMessage()
		want := ValidMessage()
		if _, err := sender.Send(context.Background(), msg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(msg, want) {
			t.Fatal("Send mutated the caller's message")
		}
	})
}

func assertDeliveryError(t *testing.T, err error, provider string, transient bool) {
	t.Helper()

	var deliveryErr *mail.DeliveryError
	if !errors.As(err, &deliveryErr) {
		t.Fatalf("want *mail.DeliveryError, got %T: %v", err, err)
	}
	if deliveryErr.Provider != provider {
		t.Fatalf("want provider %q, got %q", provider, deliveryErr.Provider)
	}
	if deliveryErr.IsTransient != transient {
		t.Fatalf("want IsTransient=%v, got %v", transient, deliveryErr.IsTransient)
	}
	if deliveryErr.Outcome != mail.SendOutcomeUnknown {
		t.Fatalf("want SendOutcomeUnknown, got %v", deliveryErr.Outcome)
	}
}
