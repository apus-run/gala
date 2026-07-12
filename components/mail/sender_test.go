package mail

import (
	"context"
	"testing"
)

func TestSenderFunc(t *testing.T) {
	var got Message
	sender := SenderFunc(func(ctx context.Context, msg Message) (Receipt, error) {
		got = msg
		return Receipt{Provider: "test", MessageID: "id-1"}, nil
	})

	receipt, err := sender.Send(context.Background(), Message{Subject: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt.Provider != "test" || receipt.MessageID != "id-1" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if got.Subject != "hi" {
		t.Fatalf("message not passed through: %+v", got)
	}
}
