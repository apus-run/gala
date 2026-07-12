//go:build integration

package smtp

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	gomail "github.com/wneessen/go-mail"

	"github.com/apus-run/gala/components/mail"
)

// newIntegrationSender 从环境变量构造真实 go-mail Client。
// 本地目标（如 Mailpit：docker run -p 1025:1025 axllent/mailpit）
// 无认证、无 TLS；设置 SMTP_TEST_USER/SMTP_TEST_PASS 时启用 PLAIN 认证，
// SMTP_TEST_TLS=mandatory 时强制 STARTTLS。
func newIntegrationSender(t *testing.T) (*Sender, string, string) {
	t.Helper()

	host := os.Getenv("SMTP_TEST_HOST")
	if host == "" {
		t.Skip("SMTP_TEST_HOST is required (e.g. a local Mailpit)")
	}
	from := os.Getenv("SMTP_TEST_FROM")
	to := os.Getenv("SMTP_TEST_TO")
	if from == "" || to == "" {
		t.Skip("SMTP_TEST_FROM and SMTP_TEST_TO are required")
	}

	options := []gomail.Option{gomail.WithTLSPolicy(gomail.NoTLS)}
	if os.Getenv("SMTP_TEST_TLS") == "mandatory" {
		options = []gomail.Option{gomail.WithTLSPolicy(gomail.TLSMandatory)}
	}
	if port := os.Getenv("SMTP_TEST_PORT"); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil {
			t.Fatalf("invalid SMTP_TEST_PORT: %v", err)
		}
		options = append(options, gomail.WithPort(n))
	}
	if user := os.Getenv("SMTP_TEST_USER"); user != "" {
		options = append(options,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(user),
			gomail.WithPassword(os.Getenv("SMTP_TEST_PASS")))
	}

	client, err := gomail.NewClient(host, options...)
	if err != nil {
		t.Fatalf("gomail.NewClient: %v", err)
	}
	sender, err := New(NewDialer(client))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return sender, from, to
}

// TestSendIntegration 验证 Headers、X-Tag、普通附件与 Inline CID 经
// 真实 SMTP 服务器发送成功（US-005）。
func TestSendIntegration(t *testing.T) {
	sender, from, to := newIntegrationSender(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	receipt, err := sender.Send(ctx, mail.Message{
		From:    mail.Address{Name: "Gala Integration", Email: from},
		To:      []mail.Address{{Email: to}},
		Subject: "gala mail smtp integration test",
		Text:    "integration test body",
		HTML:    `see <img src="cid:logo"> inline`,
		Headers: map[string]string{"X-Entity-Ref-ID": "gala-smtp-integration"},
		Tags:    map[string]string{"kind": "integration"},
		Attachments: []mail.Attachment{
			{Filename: "note.txt", ContentType: "text/plain", Content: []byte("attachment body")},
			{Filename: "logo.png", ContentType: "image/png", ContentID: "logo",
				Content: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}},
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.Provider != Provider || receipt.MessageID == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}

// TestConcurrentSendIntegration 验证同一 Sender（同一注入 Client）
// 可并发使用（SPEC §9.2/R2）；配合 -race 运行。
func TestConcurrentSendIntegration(t *testing.T) {
	sender, from, to := newIntegrationSender(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const workers = 10
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = sender.Send(ctx, mail.Message{
				From:    mail.Address{Name: "Gala Integration", Email: from},
				To:      []mail.Address{{Email: to}},
				Subject: "gala mail smtp concurrent test",
				Text:    "concurrent send " + strconv.Itoa(i),
			})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: %v", i, err)
		}
	}
}
