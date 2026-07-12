//go:build integration

package ses

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	"github.com/apus-run/gala/components/mail"
)

// TestSendIntegration 通过真实 SES API 发送到明确的测试地址
// （建议使用 SES Sandbox 与已验证身份）。它同时验证 ADR-006 的前提：
// Simple Content 对 Headers、普通附件与 Inline CID 的实际支持。
// 运行：SES_TEST_FROM=... SES_TEST_TO=... AWS_REGION=...
// AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... go test -tags=integration ./...
func TestSendIntegration(t *testing.T) {
	from := os.Getenv("SES_TEST_FROM")
	to := os.Getenv("SES_TEST_TO")
	region := os.Getenv("AWS_REGION")
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if from == "" || to == "" || region == "" || accessKeyID == "" || secretAccessKey == "" {
		t.Skip("SES_TEST_FROM, SES_TEST_TO, AWS_REGION, AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	credentials := aws.Credentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		Source:          "gala mail SES integration test",
	}
	client := sesv2.New(sesv2.Options{
		Region: region,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return credentials, nil
		}),
	})
	sender, err := New(client)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	receipt, err := sender.Send(ctx, mail.Message{
		From:    mail.Address{Name: "Gala Integration", Email: from},
		To:      []mail.Address{{Email: to}},
		Subject: "gala mail integration test",
		Text:    "integration test body",
		HTML:    `see <img src="cid:logo"> inline`,
		Headers: map[string]string{"X-Entity-Ref-ID": "gala-integration"},
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
