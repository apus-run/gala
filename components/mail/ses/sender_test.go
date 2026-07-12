package ses

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/apus-run/gala/components/mail"
	"github.com/apus-run/gala/components/mail/mailtest"
)

type fakeClient struct {
	spec  mailtest.ClientSpec
	input *sesv2.SendEmailInput
}

type clientFunc func(
	context.Context,
	*sesv2.SendEmailInput,
	...func(*sesv2.Options),
) (*sesv2.SendEmailOutput, error)

func (f clientFunc) SendEmail(
	ctx context.Context,
	params *sesv2.SendEmailInput,
	optFns ...func(*sesv2.Options),
) (*sesv2.SendEmailOutput, error) {
	return f(ctx, params, optFns...)
}

func (f *fakeClient) SendEmail(
	ctx context.Context,
	params *sesv2.SendEmailInput,
	optFns ...func(*sesv2.Options),
) (*sesv2.SendEmailOutput, error) {
	if f.spec.Called != nil {
		*f.spec.Called = true
	}
	f.input = params
	if f.spec.Err != nil {
		return nil, f.spec.Err
	}
	return &sesv2.SendEmailOutput{MessageId: ptr(f.spec.MessageID)}, nil
}

func newTestSender(t *testing.T, spec mailtest.ClientSpec, options ...Option) (*Sender, *fakeClient) {
	t.Helper()
	client := &fakeClient{spec: spec}
	sender, err := New(client, options...)
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
	if _, err := New(&fakeClient{}, WithMaxMessageBytes(-1)); err == nil {
		t.Fatal("non-positive budget must fail")
	}

	for _, test := range []struct {
		name string
		set  string
	}{
		{name: "space", set: "bad name"},
		{name: "non-ascii", set: "生产"},
		{name: "too long", set: strings.Repeat("a", maxConfigurationSet+1)},
	} {
		t.Run("configuration set "+test.name, func(t *testing.T) {
			if _, err := New(&fakeClient{}, WithConfigurationSet(test.set)); err == nil {
				t.Fatalf("WithConfigurationSet(%q) must fail", test.set)
			}
		})
	}
}

func TestMapMessage(t *testing.T) {
	sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"},
		WithConfigurationSet("production"))

	msg := mailtest.ValidMessage()
	if _, err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	input := client.input

	if want := `"Gala" <hello@example.com>`; stringValue(input.FromEmailAddress) != want {
		t.Fatalf("From = %q, want %q", stringValue(input.FromEmailAddress), want)
	}
	dest := input.Destination
	if len(dest.ToAddresses) != 1 || !strings.HasSuffix(dest.ToAddresses[0], "<to@example.com>") {
		t.Fatalf("To = %v", dest.ToAddresses)
	}
	if len(dest.CcAddresses) != 1 || dest.CcAddresses[0] != "<cc@example.com>" {
		t.Fatalf("Cc = %v", dest.CcAddresses)
	}
	if len(dest.BccAddresses) != 1 || dest.BccAddresses[0] != "<bcc@example.com>" {
		t.Fatalf("Bcc = %v", dest.BccAddresses)
	}
	if len(input.ReplyToAddresses) != 1 || input.ReplyToAddresses[0] != "<reply@example.com>" {
		t.Fatalf("ReplyTo = %v", input.ReplyToAddresses)
	}
	if stringValue(input.ConfigurationSetName) != "production" {
		t.Fatalf("ConfigurationSetName = %v", input.ConfigurationSetName)
	}

	simple := input.Content.Simple
	if stringValue(simple.Subject.Data) != msg.Subject || stringValue(simple.Subject.Charset) != "UTF-8" {
		t.Fatalf("Subject = %+v", simple.Subject)
	}
	if stringValue(simple.Body.Text.Data) != msg.Text || stringValue(simple.Body.Text.Charset) != "UTF-8" {
		t.Fatalf("Text = %+v", simple.Body.Text)
	}
	if stringValue(simple.Body.Html.Data) != msg.HTML || stringValue(simple.Body.Html.Charset) != "UTF-8" {
		t.Fatalf("Html = %+v", simple.Body.Html)
	}
	if len(simple.Headers) != 1 || stringValue(simple.Headers[0].Name) != "X-Entity-Ref-ID" {
		t.Fatalf("Headers = %v", simple.Headers)
	}
	if len(input.EmailTags) != 1 ||
		stringValue(input.EmailTags[0].Name) != "kind" || stringValue(input.EmailTags[0].Value) != "contract" {
		t.Fatalf("EmailTags = %v", input.EmailTags)
	}

	if len(simple.Attachments) != 2 {
		t.Fatalf("Attachments = %v", simple.Attachments)
	}
	plain, inline := simple.Attachments[0], simple.Attachments[1]
	if plain.ContentDisposition != types.AttachmentContentDispositionAttachment || plain.ContentId != nil {
		t.Fatalf("plain attachment = %+v", plain)
	}
	if inline.ContentDisposition != types.AttachmentContentDispositionInline || stringValue(inline.ContentId) != "logo" {
		t.Fatalf("inline attachment = %+v", inline)
	}
	for _, a := range simple.Attachments {
		if a.ContentTransferEncoding != types.AttachmentContentTransferEncodingBase64 {
			t.Fatalf("transfer encoding = %v", a.ContentTransferEncoding)
		}
	}

	// 深拷贝：修改调用方数据不影响已构造的请求。
	msg.Attachments[0].Content[0] = 'X'
	if plain.RawContent[0] != 'h' {
		t.Fatal("request must not alias caller attachment bytes")
	}
}

func TestMapBodyOmitsEmptyParts(t *testing.T) {
	sender, client := newTestSender(t, mailtest.ClientSpec{MessageID: "id-1"})
	msg := mailtest.ValidMessage()
	msg.HTML = ""
	if _, err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	body := client.input.Content.Simple.Body
	if body.Html != nil || body.Text == nil {
		t.Fatalf("Body = %+v", body)
	}
	if client.input.ConfigurationSetName != nil {
		t.Fatal("configuration set must be omitted when not configured")
	}
}

func TestMessageBudget(t *testing.T) {
	called := false
	sender, _ := newTestSender(t, mailtest.ClientSpec{Called: &called}, WithMaxMessageBytes(64))

	msg := mail.Message{
		From: mail.Address{Email: "a@example.com"},
		To:   []mail.Address{{Email: "b@example.com"}},
		Text: strings.Repeat("x", 128),
	}
	_, err := sender.Send(context.Background(), msg)
	if !errors.Is(err, mail.ErrInvalidMessage) {
		t.Fatalf("want ErrInvalidMessage, got %v", err)
	}
	if called {
		t.Fatal("client must not be called when over budget")
	}
}

func TestProviderValidationRejectsBeforeClient(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*mail.Message)
	}{
		{
			name: "too many recipients",
			mutate: func(msg *mail.Message) {
				msg.To = make([]mail.Address, maxRecipients+1)
				for i := range msg.To {
					msg.To[i] = mail.Address{Email: "to@example.com"}
				}
			},
		},
		{name: "non-ascii from", mutate: func(msg *mail.Message) { msg.From.Email = "发件人@example.com" }},
		{name: "non-ascii to", mutate: func(msg *mail.Message) { msg.To[0].Email = "收件人@example.com" }},
		{name: "non-ascii cc", mutate: func(msg *mail.Message) { msg.Cc[0].Email = "用户@example.com" }},
		{name: "non-ascii bcc", mutate: func(msg *mail.Message) { msg.Bcc[0].Email = "用户@example.com" }},
		{name: "non-ascii reply-to", mutate: func(msg *mail.Message) { msg.ReplyTo.Email = "用户@example.com" }},
		{
			name: "too many headers",
			mutate: func(msg *mail.Message) {
				msg.Headers = make(map[string]string, maxHeaders+1)
				for i := 0; i <= maxHeaders; i++ {
					msg.Headers[fmt.Sprintf("X-Test-%02d", i)] = "value"
				}
			},
		},
		{name: "header name contains colon", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"X:Bad": "value"} }},
		{name: "header name contains control", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"X\tBad": "value"} }},
		{name: "header name too long", mutate: func(msg *mail.Message) {
			msg.Headers = map[string]string{strings.Repeat("x", maxHeaderNameLength+1): "value"}
		}},
		{name: "header value empty", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"X-Test": ""} }},
		{name: "header value contains control", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"X-Test": "bad\tvalue"} }},
		{name: "header value too long", mutate: func(msg *mail.Message) {
			msg.Headers = map[string]string{"X-Test": strings.Repeat("x", maxHeaderValueLength+1)}
		}},
		{name: "header combined too long", mutate: func(msg *mail.Message) {
			msg.Headers = map[string]string{"XX": strings.Repeat("x", maxHeaderValueLength)}
		}},
		{name: "content-disposition reserved", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"content-disposition": "inline"} }},
		{name: "date reserved", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"date": "today"} }},
		{name: "message-id reserved", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"message-id": "id"} }},
		{name: "return-path reserved", mutate: func(msg *mail.Message) { msg.Headers = map[string]string{"return-path": "bounce@example.com"} }},
		{name: "tag name empty", mutate: func(msg *mail.Message) { msg.Tags = map[string]string{"": "value"} }},
		{name: "tag value empty", mutate: func(msg *mail.Message) { msg.Tags = map[string]string{"name": ""} }},
		{name: "tag name invalid", mutate: func(msg *mail.Message) { msg.Tags = map[string]string{"bad tag": "value"} }},
		{name: "tag value invalid", mutate: func(msg *mail.Message) { msg.Tags = map[string]string{"name": "bad/value"} }},
		{name: "tag name too long", mutate: func(msg *mail.Message) { msg.Tags = map[string]string{strings.Repeat("x", maxTagLength+1): "value"} }},
		{name: "tag value too long", mutate: func(msg *mail.Message) { msg.Tags = map[string]string{"name": strings.Repeat("x", maxTagLength+1)} }},
		{name: "attachment filename too long", mutate: func(msg *mail.Message) { msg.Attachments[0].Filename = strings.Repeat("a", maxAttachmentName+1) }},
		{name: "attachment content id too long", mutate: func(msg *mail.Message) { msg.Attachments[0].ContentID = strings.Repeat("a", maxAttachmentContentID+1) }},
		{name: "attachment content type too long", mutate: func(msg *mail.Message) { msg.Attachments[0].ContentType = strings.Repeat("a", maxAttachmentType+1) }},
		{name: "unsupported exe attachment", mutate: func(msg *mail.Message) { msg.Attachments[0].Filename = "payload.EXE" }},
		{name: "unsupported ade attachment", mutate: func(msg *mail.Message) { msg.Attachments[0].Filename = "payload.ADE" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			sender, _ := newTestSender(t, mailtest.ClientSpec{Called: &called})
			msg := mailtest.ValidMessage()
			test.mutate(&msg)

			_, err := sender.Send(context.Background(), msg)
			if !errors.Is(err, mail.ErrInvalidMessage) {
				t.Fatalf("want ErrInvalidMessage, got %v", err)
			}
			if called {
				t.Fatal("client must not be called for an invalid SES message")
			}
			if mail.MayHaveBeenAccepted(err) {
				t.Fatal("pre-call SES validation error must not count as possibly accepted")
			}
		})
	}
}

func TestProviderValidationAcceptsBoundaries(t *testing.T) {
	msg := mailtest.ValidMessage()
	msg.To = make([]mail.Address, maxRecipients)
	for i := range msg.To {
		msg.To[i] = mail.Address{Email: "to@example.com"}
	}
	msg.Cc = nil
	msg.Bcc = nil
	msg.Headers = make(map[string]string, maxHeaders)
	msg.Headers["X"] = strings.Repeat("v", maxHeaderValueLength)
	for i := 1; i < maxHeaders; i++ {
		msg.Headers[fmt.Sprintf("X-Test-%02d", i)] = "value"
	}
	msg.Tags = map[string]string{
		strings.Repeat("n", maxTagLength): strings.Repeat("v", maxTagLength),
	}
	msg.Attachments = []mail.Attachment{{
		Filename:    strings.Repeat("a", maxAttachmentName-len(".txt")) + ".txt",
		ContentType: strings.Repeat("t", maxAttachmentType),
		ContentID:   strings.Repeat("i", maxAttachmentContentID),
		Content:     []byte("content"),
	}}

	sender, _ := newTestSender(t, mailtest.ClientSpec{MessageID: "id-boundary"},
		WithConfigurationSet(strings.Repeat("c", maxConfigurationSet)))
	if _, err := sender.Send(context.Background(), msg); err != nil {
		t.Fatalf("boundary message must be accepted: %v", err)
	}
}

func TestSESHardMessageLimitRejectsBeforeClient(t *testing.T) {
	called := false
	sender, _ := newTestSender(t, mailtest.ClientSpec{Called: &called},
		WithMaxMessageBytes(64<<20))
	msg := mail.Message{
		From: mail.Address{Email: "from@example.com"},
		To:   []mail.Address{{Email: "to@example.com"}},
		Text: "body",
		Attachments: []mail.Attachment{{
			Filename: "archive.bin",
			Content:  make([]byte, 30<<20),
		}},
	}

	_, err := sender.Send(context.Background(), msg)
	if !errors.Is(err, mail.ErrInvalidMessage) {
		t.Fatalf("want ErrInvalidMessage, got %v", err)
	}
	if called {
		t.Fatal("client must not be called when encoded message exceeds SES hard limit")
	}
}

func TestInvalidSuccessResponse(t *testing.T) {
	tests := []struct {
		name   string
		output *sesv2.SendEmailOutput
	}{
		{name: "nil output"},
		{name: "nil message id", output: &sesv2.SendEmailOutput{}},
		{name: "empty message id", output: &sesv2.SendEmailOutput{MessageId: ptr("")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientFunc(func(
				context.Context,
				*sesv2.SendEmailInput,
				...func(*sesv2.Options),
			) (*sesv2.SendEmailOutput, error) {
				return test.output, nil
			})
			sender, err := New(client)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			_, err = sender.Send(context.Background(), mailtest.ValidMessage())
			var deliveryErr *mail.DeliveryError
			if !errors.As(err, &deliveryErr) {
				t.Fatalf("want DeliveryError, got %v", err)
			}
			if deliveryErr.Code != "invalid_response" ||
				deliveryErr.Outcome != mail.SendOutcomeUnknown || deliveryErr.IsTransient {
				t.Fatalf("unexpected DeliveryError: %+v", deliveryErr)
			}
			if !mail.MayHaveBeenAccepted(err) {
				t.Fatal("invalid success response must preserve unknown acceptance outcome")
			}
		})
	}
}

func TestErrorMapping(t *testing.T) {
	send := func(t *testing.T, clientErr error) *mail.DeliveryError {
		t.Helper()
		sender, _ := newTestSender(t, mailtest.ClientSpec{Err: clientErr})
		_, err := sender.Send(context.Background(), mailtest.ValidMessage())
		var deliveryErr *mail.DeliveryError
		if !errors.As(err, &deliveryErr) {
			t.Fatalf("want DeliveryError, got %v", err)
		}
		if deliveryErr.Outcome != mail.SendOutcomeUnknown {
			t.Fatalf("all post-call ses failures must be SendOutcomeUnknown: %+v", deliveryErr)
		}
		return deliveryErr
	}

	wrap := func(err error) error {
		return &smithy.OperationError{ServiceID: "SESv2", OperationName: "SendEmail", Err: err}
	}

	t.Run("throttling is transient", func(t *testing.T) {
		deliveryErr := send(t, wrap(&types.TooManyRequestsException{Message: ptr("slow down")}))
		if !deliveryErr.IsTransient || deliveryErr.Code != "TooManyRequestsException" {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})

	t.Run("limit exceeded is transient", func(t *testing.T) {
		deliveryErr := send(t, wrap(&types.LimitExceededException{Message: ptr("limit")}))
		if !deliveryErr.IsTransient {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})

	t.Run("final http 5xx is transient", func(t *testing.T) {
		responseErr := wrap(&smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 502}},
			Err:      errors.New("bad gateway"),
		})
		deliveryErr := send(t, responseErr)
		if !deliveryErr.IsTransient || deliveryErr.Code != "http_5xx" {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})

	t.Run("typed 4xx keeps its code and http status is ignored", func(t *testing.T) {
		responseErr := wrap(&smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 400}},
			Err:      &types.MessageRejected{Message: ptr("nope")},
		})
		deliveryErr := send(t, responseErr)
		if deliveryErr.IsTransient || deliveryErr.Code != "MessageRejected" {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})

	for _, apiErr := range []smithy.APIError{
		&types.MessageRejected{Message: ptr("x")},
		&types.MailFromDomainNotVerifiedException{Message: ptr("x")},
		&types.AccountSuspendedException{Message: ptr("x")},
		&types.SendingPausedException{Message: ptr("x")},
		&types.BadRequestException{Message: ptr("x")},
		&types.NotFoundException{Message: ptr("x")},
	} {
		t.Run("permanent "+apiErr.ErrorCode(), func(t *testing.T) {
			deliveryErr := send(t, wrap(apiErr))
			if deliveryErr.IsTransient || deliveryErr.Code != apiErr.ErrorCode() {
				t.Fatalf("classification wrong: %+v", deliveryErr)
			}
		})
	}

	t.Run("network timeout is transient", func(t *testing.T) {
		deliveryErr := send(t, wrap(&timeoutError{}))
		if !deliveryErr.IsTransient || deliveryErr.Code != "timeout" {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})

	t.Run("permanent net error is not transient", func(t *testing.T) {
		deliveryErr := send(t, wrap(&permanentNetError{}))
		if deliveryErr.IsTransient {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})

	t.Run("unknown error is conservative", func(t *testing.T) {
		deliveryErr := send(t, errors.New("boom"))
		if deliveryErr.IsTransient || deliveryErr.Code != "" {
			t.Fatalf("classification wrong: %+v", deliveryErr)
		}
	})
}

type timeoutError struct{}

func (*timeoutError) Error() string   { return "i/o timeout" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }

type permanentNetError struct{}

func (*permanentNetError) Error() string   { return "no such host" }
func (*permanentNetError) Timeout() bool   { return false }
func (*permanentNetError) Temporary() bool { return false }
