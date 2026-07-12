package mailtest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/apus-run/gala/components/mail"
)

func TestRecorderDeepCopiesMessages(t *testing.T) {
	recorder := &Recorder{Receipt: mail.Receipt{Provider: "fake", MessageID: "id-1"}}

	msg := ValidMessage()
	want := ValidMessage()

	receipt, err := recorder.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt != recorder.Receipt {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}

	// 调用方在 Send 之后修改原消息，不得影响已记录内容。
	msg.To[0].Email = "changed@example.com"
	msg.Cc[0].Email = "changed-cc@example.com"
	msg.Bcc[0].Email = "changed-bcc@example.com"
	msg.Headers["X-Entity-Ref-ID"] = "changed"
	msg.Tags["kind"] = "changed"
	msg.Attachments[0].Filename = "changed.txt"
	msg.Attachments[0].Content[0] = 'X'
	msg.ReplyTo.Email = "changed@example.com"

	recorded := recorder.Messages()
	if len(recorded) != 1 {
		t.Fatalf("want 1 message, got %d", len(recorded))
	}
	if !reflect.DeepEqual(recorded[0], want) {
		t.Fatalf("recorded message affected by caller mutation:\ngot  %+v\nwant %+v", recorded[0], want)
	}

	// 读取结果同样是深拷贝：修改返回值不影响后续读取。
	recorded[0].Subject = "changed"
	recorded[0].To[0].Email = "returned@example.com"
	recorded[0].Cc[0].Email = "returned-cc@example.com"
	recorded[0].Bcc[0].Email = "returned-bcc@example.com"
	recorded[0].ReplyTo.Email = "returned-reply@example.com"
	recorded[0].Headers["X-Entity-Ref-ID"] = "returned"
	recorded[0].Tags["kind"] = "returned"
	recorded[0].Attachments[0].Filename = "returned.txt"
	recorded[0].Attachments[0].Content[0] = 'Y'
	again := recorder.Messages()
	if !reflect.DeepEqual(again[0], want) {
		t.Fatal("Messages() result is not isolated from callers")
	}
}

func TestRecorderReturnsConfiguredError(t *testing.T) {
	sentinel := errors.New("configured failure")
	recorder := &Recorder{Err: sentinel}

	_, err := recorder.Send(context.Background(), ValidMessage())
	if !errors.Is(err, sentinel) {
		t.Fatalf("want configured error, got %v", err)
	}
	if len(recorder.Messages()) != 1 {
		t.Fatal("message must still be recorded on configured error")
	}
}

func TestRecorderHonoursContext(t *testing.T) {
	recorder := &Recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := recorder.Send(ctx, ValidMessage())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if len(recorder.Messages()) != 0 {
		t.Fatal("cancelled send must not be recorded")
	}
}
