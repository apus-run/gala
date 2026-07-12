package resend

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	resendgo "github.com/resend/resend-go/v3"

	"github.com/apus-run/gala/components/mail"
	"github.com/apus-run/gala/components/mail/mailtest"
)

type fakeClient struct {
	spec      mailtest.ClientSpec
	request   *resendgo.SendEmailRequest
	options   *resendgo.SendEmailOptions
	output    *resendgo.SendEmailResponse
	useOutput bool
}

func (f *fakeClient) SendWithOptions(
	ctx context.Context,
	params *resendgo.SendEmailRequest,
	options *resendgo.SendEmailOptions,
) (*resendgo.SendEmailResponse, error) {
	if f.spec.Called != nil {
		*f.spec.Called = true
	}
	f.request = params
	f.options = options
	if f.spec.Err != nil {
		return nil, f.spec.Err
	}
	if f.useOutput {
		return f.output, nil
	}
	return &resendgo.SendEmailResponse{Id: f.spec.MessageID}, nil
}

func newTestSender(t *testing.T, spec mailtest.ClientSpec) (*Sender, *fakeClient) {
	t.Helper()
	client := &fakeClient{spec: spec}
	sender, err := New(client)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return sender, client
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
	if _, err := New(&fakeClient{}, WithMaxMessageBytes(0)); err == nil {
		t.Fatal("non-positive budget must fail")
	}
	for name, apiKey := range map[string]string{
		"empty":             "",
		"whitespace":        " \t ",
		"empty quotes":      "''",
		"quoted whitespace": " ' ' ",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewFromAPIKey(apiKey); err == nil {
				t.Fatal("semantically empty api key must fail")
			}
		})
	}
	if _, err := NewFromAPIKey("re_test_key"); err != nil {
		t.Fatalf("NewFromAPIKey: %v", err)
	}
	if _, err := NewFromAPIKey(" 're_test_key' "); err != nil {
		t.Fatalf("NewFromAPIKey with outer quotes: %v", err)
	}
}

func TestMapMessage(t *testing.T) {
	sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})

	msg := mailtest.ValidMessage()
	if _, err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	request := client.request

	if want := `"Gala" <hello@example.com>`; request.From != want {
		t.Fatalf("From = %q, want %q", request.From, want)
	}
	if len(request.To) != 1 || !strings.HasSuffix(request.To[0], "<to@example.com>") {
		t.Fatalf("To = %v", request.To)
	}
	if len(request.Cc) != 1 || request.Cc[0] != "<cc@example.com>" {
		t.Fatalf("Cc = %v", request.Cc)
	}
	if len(request.Bcc) != 1 || request.Bcc[0] != "<bcc@example.com>" {
		t.Fatalf("Bcc = %v", request.Bcc)
	}
	if request.ReplyTo != "<reply@example.com>" {
		t.Fatalf("ReplyTo = %q", request.ReplyTo)
	}
	if request.Subject != msg.Subject || request.Text != msg.Text || request.Html != msg.HTML {
		t.Fatal("subject or bodies not mapped independently")
	}
	if request.Headers["X-Entity-Ref-ID"] != "abc" {
		t.Fatalf("Headers = %v", request.Headers)
	}
	if len(request.Tags) != 1 || request.Tags[0] != (resendgo.Tag{Name: "kind", Value: "contract"}) {
		t.Fatalf("Tags = %v", request.Tags)
	}
	if len(request.Attachments) != 2 {
		t.Fatalf("Attachments = %v", request.Attachments)
	}
	if request.Attachments[0].ContentId != "" || request.Attachments[1].ContentId != "logo" {
		t.Fatal("ContentId mapping wrong")
	}

	// 深拷贝：修改调用方数据不影响已构造的请求。
	msg.Headers["X-Entity-Ref-ID"] = "changed"
	msg.Attachments[0].Content[0] = 'X'
	if request.Headers["X-Entity-Ref-ID"] != "abc" || request.Attachments[0].Content[0] != 'h' {
		t.Fatal("request must not alias caller data")
	}
}

func TestTagsSortedByKey(t *testing.T) {
	tags := map[string]string{"b": "2", "a": "1", "c": "3"}
	got := mapTags(tags)
	want := []resendgo.Tag{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}, {Name: "c", Value: "3"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tags not sorted: %v", got)
		}
	}
}

func TestProviderValidation(t *testing.T) {
	tooManyRecipients := make([]mail.Address, maxResendRecipients+1)
	for i := range tooManyRecipients {
		tooManyRecipients[i] = mail.Address{Email: "to@example.com"}
	}

	tests := []struct {
		name   string
		mutate func(*mail.Message)
	}{
		{
			name: "to is required even when cc and bcc are present",
			mutate: func(msg *mail.Message) {
				msg.To = nil
			},
		},
		{
			name: "too many to recipients",
			mutate: func(msg *mail.Message) {
				msg.To = tooManyRecipients
			},
		},
		{
			name: "empty tag name",
			mutate: func(msg *mail.Message) {
				msg.Tags = map[string]string{"": "value"}
			},
		},
		{
			name: "empty tag value",
			mutate: func(msg *mail.Message) {
				msg.Tags = map[string]string{"name": ""}
			},
		},
		{
			name: "tag name contains unsupported character",
			mutate: func(msg *mail.Message) {
				msg.Tags = map[string]string{"bad name": "value"}
			},
		},
		{
			name: "tag value contains unsupported character",
			mutate: func(msg *mail.Message) {
				msg.Tags = map[string]string{"name": "值"}
			},
		},
		{
			name: "tag name too long",
			mutate: func(msg *mail.Message) {
				msg.Tags = map[string]string{strings.Repeat("n", maxResendTagLength+1): "value"}
			},
		},
		{
			name: "tag value too long",
			mutate: func(msg *mail.Message) {
				msg.Tags = map[string]string{"name": strings.Repeat("v", maxResendTagLength+1)}
			},
		},
		{
			name: "content id too long",
			mutate: func(msg *mail.Message) {
				msg.Attachments[1].ContentID = strings.Repeat("c", maxResendContentIDLength)
			},
		},
		{
			name: "unsupported attachment extension is case insensitive",
			mutate: func(msg *mail.Message) {
				msg.Attachments[0].Filename = "payload.EXE"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			sender, _ := newTestSender(t, mailtest.ClientSpec{Called: &called})
			msg := mailtest.ValidMessage()
			tt.mutate(&msg)

			_, err := sender.Send(context.Background(), msg)
			if !errors.Is(err, mail.ErrInvalidMessage) {
				t.Fatalf("want ErrInvalidMessage, got %v", err)
			}
			if called {
				t.Fatal("client must not be called for a provider-invalid message")
			}
		})
	}

	t.Run("provider boundaries accepted", func(t *testing.T) {
		to := make([]mail.Address, maxResendRecipients)
		for i := range to {
			to[i] = mail.Address{Email: "to@example.com"}
		}
		msg := mailtest.ValidMessage()
		msg.To = to
		msg.Subject = ""
		msg.Tags = map[string]string{
			strings.Repeat("n", maxResendTagLength): strings.Repeat("v", maxResendTagLength),
		}
		msg.Attachments[1].ContentID = strings.Repeat("界", maxResendContentIDLength-1)

		sender, _ := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		if _, err := sender.Send(context.Background(), msg); err != nil {
			t.Fatalf("boundary-valid message: %v", err)
		}
	})
}

func TestIdempotencyKey(t *testing.T) {
	t.Run("passed through", func(t *testing.T) {
		sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		_, err := sender.SendWithOptions(context.Background(), mailtest.ValidMessage(),
			SendOptions{IdempotencyKey: "welcome/42"})
		if err != nil {
			t.Fatalf("SendWithOptions: %v", err)
		}
		if client.options.IdempotencyKey != "welcome/42" {
			t.Fatalf("options = %+v", client.options)
		}
	})

	t.Run("empty key disables idempotency", func(t *testing.T) {
		sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		if _, err := sender.Send(context.Background(), mailtest.ValidMessage()); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if client.options.IdempotencyKey != "" {
			t.Fatalf("options = %+v", client.options)
		}
	})

	for name, key := range map[string]string{
		"too long":     strings.Repeat("k", 257),
		"control char": "abc\ndef",
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			sender, _ := newTestSender(t, mailtest.ClientSpec{Called: &called})
			_, err := sender.SendWithOptions(context.Background(), mailtest.ValidMessage(),
				SendOptions{IdempotencyKey: key})
			if !errors.Is(err, mail.ErrInvalidMessage) {
				t.Fatalf("want ErrInvalidMessage, got %v", err)
			}
			if called {
				t.Fatal("client must not be called with an invalid idempotency key")
			}
		})
	}

	t.Run("max length accepted", func(t *testing.T) {
		sender, _ := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
		_, err := sender.SendWithOptions(context.Background(), mailtest.ValidMessage(),
			SendOptions{IdempotencyKey: strings.Repeat("k", 256)})
		if err != nil {
			t.Fatalf("256-char key must be accepted: %v", err)
		}
	})
}

func TestMessageBudget(t *testing.T) {
	called := false
	client := &fakeClient{spec: mailtest.ClientSpec{Called: &called, MessageID: "id-1"}}
	sender, err := New(client, WithMaxMessageBytes(64))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	msg := mail.Message{
		From: mail.Address{Email: "a@example.com"},
		To:   []mail.Address{{Email: "b@example.com"}},
		Text: strings.Repeat("x", 128),
	}
	_, err = sender.Send(context.Background(), msg)
	if !errors.Is(err, mail.ErrInvalidMessage) {
		t.Fatalf("want ErrInvalidMessage, got %v", err)
	}
	if called {
		t.Fatal("client must not be called when over budget")
	}

	msg.Text = "small"
	if _, err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("within budget: %v", err)
	}
}

func TestProviderMessageSizeCannotBeRaised(t *testing.T) {
	called := false
	client := &fakeClient{spec: mailtest.ClientSpec{Called: &called}}
	sender, err := New(client, WithMaxMessageBytes(64<<20))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	msg := mailtest.ValidMessage()
	msg.Attachments = []mail.Attachment{{
		Filename: "large.bin",
		Content:  make([]byte, 30<<20),
	}}
	_, err = sender.Send(context.Background(), msg)
	if !errors.Is(err, mail.ErrInvalidMessage) {
		t.Fatalf("want ErrInvalidMessage, got %v", err)
	}
	if called {
		t.Fatal("client must not be called when the encoded message exceeds Resend's hard limit")
	}
}

func TestErrorMapping(t *testing.T) {
	send := func(t *testing.T, clientErr error) error {
		t.Helper()
		sender, _ := newTestSender(t, mailtest.ClientSpec{Err: clientErr})
		_, err := sender.Send(context.Background(), mailtest.ValidMessage())
		if err == nil {
			t.Fatal("want error")
		}
		return err
	}

	t.Run("rate limit with retry-after", func(t *testing.T) {
		err := send(t, &resendgo.RateLimitError{Message: "slow down", RetryAfter: "7"})
		var deliveryErr *mail.DeliveryError
		if !errors.As(err, &deliveryErr) {
			t.Fatalf("want DeliveryError, got %v", err)
		}
		if !deliveryErr.IsTransient || deliveryErr.Outcome != mail.SendOutcomeNotAccepted {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
		if deliveryErr.RetryAfter != 7*time.Second {
			t.Fatalf("RetryAfter = %v", deliveryErr.RetryAfter)
		}
		if mail.MayHaveBeenAccepted(err) {
			t.Fatal("429 means the provider did not accept the request")
		}
	})

	for name, retryAfter := range map[string]string{
		"missing": "", "garbage": "soon", "negative": "-5", "zero": "0", "float": "1.5",
		"duration overflow": "9223372036854775807",
	} {
		t.Run("retry-after "+name, func(t *testing.T) {
			err := send(t, &resendgo.RateLimitError{RetryAfter: retryAfter})
			var deliveryErr *mail.DeliveryError
			if !errors.As(err, &deliveryErr) || deliveryErr.RetryAfter != 0 {
				t.Fatalf("want RetryAfter 0, got %v", err)
			}
		})
	}

	t.Run("timeout is transient unknown", func(t *testing.T) {
		err := send(t, &timeoutError{})
		if !mail.IsTransient(err) || !mail.MayHaveBeenAccepted(err) {
			t.Fatalf("timeout must be transient with unknown outcome: %v", err)
		}
	})

	t.Run("non-timeout net error is not transient", func(t *testing.T) {
		err := send(t, &permanentNetError{})
		if mail.IsTransient(err) {
			t.Fatalf("permanent net error must not be transient: %v", err)
		}
	})

	t.Run("unknown error is conservative", func(t *testing.T) {
		err := send(t, errors.New("resend: something else"))
		if mail.IsTransient(err) {
			t.Fatal("unknown errors must not be transient")
		}
		if !mail.MayHaveBeenAccepted(err) {
			t.Fatal("unknown errors must keep SendOutcomeUnknown")
		}
	})
}

func TestInvalidSuccessResponse(t *testing.T) {
	tests := []struct {
		name   string
		output *resendgo.SendEmailResponse
	}{
		{name: "nil response"},
		{name: "empty message id", output: &resendgo.SendEmailResponse{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{useOutput: true, output: tt.output}
			sender, err := New(client)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			receipt, err := sender.Send(context.Background(), mailtest.ValidMessage())
			if receipt != (mail.Receipt{}) {
				t.Fatalf("receipt = %+v", receipt)
			}
			var deliveryErr *mail.DeliveryError
			if !errors.As(err, &deliveryErr) {
				t.Fatalf("want DeliveryError, got %v", err)
			}
			if deliveryErr.Code != "invalid_response" ||
				deliveryErr.Outcome != mail.SendOutcomeUnknown || deliveryErr.IsTransient {
				t.Fatalf("invalid response classification = %+v", deliveryErr)
			}
			if !mail.MayHaveBeenAccepted(err) {
				t.Fatal("a malformed success response may still mean the request was accepted")
			}
		})
	}
}

type timeoutError struct{}

func (*timeoutError) Error() string   { return "i/o timeout" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }

type permanentNetError struct{}

func (*permanentNetError) Error() string   { return "no such host" }
func (*permanentNetError) Timeout() bool   { return false }
func (*permanentNetError) Temporary() bool { return false }
