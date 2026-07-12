//go:build integration

package resend

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mail"
)

// TestSendIntegration 通过真实 Resend API 发送到明确的测试地址。
// 运行：RESEND_API_KEY=... RESEND_TEST_TO=... go test -tags=integration ./...
func TestSendIntegration(t *testing.T) {
	apiKey := os.Getenv("RESEND_API_KEY")
	to := os.Getenv("RESEND_TEST_TO")
	from := os.Getenv("RESEND_TEST_FROM")
	if apiKey == "" || to == "" || from == "" {
		t.Skip("RESEND_API_KEY, RESEND_TEST_TO and RESEND_TEST_FROM are required")
	}

	sender, err := NewFromAPIKey(apiKey)
	if err != nil {
		t.Fatalf("NewFromAPIKey: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	receipt, err := sender.Send(ctx, mail.Message{
		From:    mail.Address{Name: "Gala Integration", Email: from},
		To:      []mail.Address{{Email: to}},
		Subject: "gala mail integration test",
		Text:    "integration test body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.Provider != Provider || receipt.MessageID == "" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}
