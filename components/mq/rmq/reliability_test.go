package rmq

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apus-run/gala/components/mq"
)

type recordingProducer struct {
	rocketmq.Producer
	messages []*primitive.Message
	result   *primitive.SendResult
	err      error
	callback func(context.Context, *primitive.SendResult, error)
	ctx      context.Context
}

func (p *recordingProducer) SendSync(ctx context.Context, msgs ...*primitive.Message) (*primitive.SendResult, error) {
	p.ctx, p.messages = ctx, msgs
	return p.result, p.err
}
func (p *recordingProducer) SendAsync(ctx context.Context, cb func(context.Context, *primitive.SendResult, error), msgs ...*primitive.Message) error {
	p.ctx, p.messages, p.callback = ctx, msgs, cb
	return p.err
}

func TestSendSnapshot(t *testing.T) {
	for _, mode := range []string{"sync", "batch", "async"} {
		t.Run(mode, func(t *testing.T) {
			sdk := &recordingProducer{result: &primitive.SendResult{Status: primitive.SendOK}}
			p := &Producer{producer: sdk}
			msg := mq.NewMessage("events", []byte("body")).WithProperties(map[string]string{"user": "original"})
			var err error
			switch mode {
			case "sync":
				_, err = p.Send(context.Background(), msg)
			case "batch":
				_, err = p.SendBatch(context.Background(), []*mq.Message{msg, msg})
			case "async":
				err = p.SendAsync(context.Background(), func(context.Context, mq.SendResponse, error) {}, msg)
			}
			if err != nil {
				t.Fatal(err)
			}
			msg.Body[0] = 'X'
			msg.Properties["user"] = "changed"
			for _, sent := range sdk.messages {
				if string(sent.Body) != "body" || sent.GetProperty("user") != "original" {
					t.Fatal("send retains mutable caller data")
				}
				sent.WithProperty("sdk", "value")
			}
			if _, ok := msg.Properties["sdk"]; ok {
				t.Fatal("SDK mutated caller properties")
			}
		})
	}
}

func TestSendMetadata(t *testing.T) {
	sdk := &recordingProducer{result: &primitive.SendResult{Status: primitive.SendOK}}
	p := &Producer{producer: sdk}
	properties := map[string]string{"user": "value", primitive.PropertyTags: "old", primitive.PropertyShardingKey: "old"}
	msg := mq.NewOrderlyMessage("events", "account-1", []byte("body")).WithTag("created").WithProperties(properties).WithDeferDuration(time.Second + time.Nanosecond)
	if _, err := p.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	sent := sdk.messages[0]
	if sent.GetTags() != "created" || sent.GetShardingKey() != "account-1" || sent.GetProperty("user") != "value" || sent.GetProperty(primitive.PropertyDelayTimeLevel) != "2" {
		t.Fatalf("lost metadata: %v", sent.GetProperties())
	}
	if properties[primitive.PropertyTags] != "old" || properties[primitive.PropertyDelayTimeLevel] != "" {
		t.Fatal("input properties modified")
	}
}

func TestDelayNeverRoundsDown(t *testing.T) {
	p := &Producer{}
	for i, seconds := range delayLevels {
		d := time.Duration(seconds) * time.Second
		if got := p.toDelayLevel(d); got != i+1 {
			t.Fatalf("boundary %v: level %d", d, got)
		}
		if i+1 < len(delayLevels) {
			if got := p.toDelayLevel(d + time.Nanosecond); got != i+2 {
				t.Fatalf("%v + 1ns rounded down to level %d", d, got)
			}
		}
	}
}

func TestSendResult(t *testing.T) {
	transportErr := errors.New("transport error")
	for _, mode := range []string{"sync", "batch", "async"} {
		for _, tc := range []struct {
			name      string
			result    *primitive.SendResult
			err       error
			wantError bool
		}{
			{"ok", &primitive.SendResult{Status: primitive.SendOK, MsgID: "id", QueueOffset: 42}, nil, false},
			{"disk_timeout", &primitive.SendResult{Status: primitive.SendFlushDiskTimeout, MsgID: "id", QueueOffset: 42}, nil, true},
			{"slave_timeout", &primitive.SendResult{Status: primitive.SendFlushSlaveTimeout}, nil, true},
			{"slave_unavailable", &primitive.SendResult{Status: primitive.SendSlaveNotAvailable}, nil, true},
			{"nil_result", nil, nil, true},
			{"transport", nil, transportErr, true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				sdk := &recordingProducer{result: tc.result, err: tc.err}
				p := &Producer{producer: sdk}
				ctx := context.Background()
				msg := mq.NewMessage("events", []byte("body"))
				var resp mq.SendResponse
				var err error
				switch mode {
				case "sync":
					resp, err = p.Send(ctx, msg)
				case "batch":
					resp, err = p.SendBatch(ctx, []*mq.Message{msg, msg})
				case "async":
					sdk.err = nil
					calls := 0
					if e := p.SendAsync(ctx, func(got context.Context, r mq.SendResponse, e error) {
						calls++
						resp, err = r, e
						if got != ctx {
							t.Fatal("callback context changed")
						}
					}, msg); e != nil {
						t.Fatal(e)
					}
					sdk.callback(ctx, tc.result, tc.err)
					if calls != 1 {
						t.Fatalf("callbacks=%d", calls)
					}
				}
				if sdk.ctx != ctx {
					t.Fatal("send context changed")
				}
				if (err != nil) != tc.wantError {
					t.Fatalf("error=%v wantError=%v", err, tc.wantError)
				}
				if tc.err != nil && !errors.Is(err, tc.err) {
					t.Fatalf("transport error lost: %v", err)
				}
				if tc.result != nil && (resp.MessageID != tc.result.MsgID || resp.Offset != tc.result.QueueOffset) {
					t.Fatalf("result lost: %+v", resp)
				}
			})
		}
	}
}

func TestSendValidation(t *testing.T) {
	valid := mq.NewMessage("events", []byte("body"))
	for _, tc := range []struct {
		name     string
		messages []*mq.Message
	}{
		{"empty", nil}, {"nil_message", []*mq.Message{nil}},
		{"empty_topic", []*mq.Message{{}}},
		{"negative_delay", []*mq.Message{mq.NewDeferMessage("events", -1, nil)}},
		{"mixed_topics", []*mq.Message{valid, mq.NewMessage("other", nil)}},
		{"delayed_batch", []*mq.Message{valid, mq.NewDeferMessage("events", time.Second, nil)}},
		{"orderly_batch", []*mq.Message{valid, mq.NewOrderlyMessage("events", "key", nil)}},
		{"raw_delayed_batch", []*mq.Message{valid, mq.NewMessage("events", nil).WithProperties(map[string]string{primitive.PropertyDelayTimeLevel: "1"})}},
		{"raw_orderly_batch", []*mq.Message{valid, mq.NewMessage("events", nil).WithProperties(map[string]string{primitive.PropertyShardingKey: "key"})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sdk := &recordingProducer{result: &primitive.SendResult{}}
			p := &Producer{producer: sdk}
			if _, err := p.SendBatch(context.Background(), tc.messages); err == nil {
				t.Fatal("invalid batch accepted")
			}
			if len(tc.messages) == 1 {
				if _, err := p.Send(context.Background(), tc.messages[0]); err == nil {
					t.Fatal("invalid message accepted")
				}
				if err := p.SendAsync(context.Background(), func(context.Context, mq.SendResponse, error) {}, tc.messages[0]); err == nil {
					t.Fatal("invalid async message accepted")
				}
			}
			if sdk.messages != nil {
				t.Fatal("invalid input reached SDK")
			}
		})
	}
	p := &Producer{}
	if err := p.SendAsync(context.Background(), nil, valid); err == nil {
		t.Fatal("nil callback accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Send(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := p.SendBatch(ctx, []*mq.Message{valid}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.SendAsync(ctx, func(context.Context, mq.SendResponse, error) {}, valid); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// Type inference lets the SDK's options be exercised without naming its private options type.
func applyOptions[T any, F ~func(*T)](options []F) *T {
	out := new(T)
	for _, option := range options {
		option(out)
	}
	return out
}

func TestOrderlyRouting(t *testing.T) {
	opts, err := producerOptions(mq.ProducerConfig{Addr: []string{"127.0.0.1:9876"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := applyOptions(opts)
	if cfg.Selector == nil {
		t.Fatal("missing queue selector")
	}
	queues := []*primitive.MessageQueue{{QueueId: 0}, {QueueId: 1}, {QueueId: 2}}
	p := &Producer{}
	for _, key := range []string{"account-1", "account-2", "account-3"} {
		msg := mq.NewOrderlyMessage("events", key, nil).WithProperties(map[string]string{"user": "value"})
		first := cfg.Selector.Select(p.convertMessage(msg), queues, "")
		for range 100 {
			if got := cfg.Selector.Select(p.convertMessage(msg), queues, "previous-broker"); got != first {
				t.Fatal("same key routed to different queues")
			}
		}
	}
}

func TestResolverOwnsAddresses(t *testing.T) {
	addrs := []string{"127.0.0.1:9876", "[::1]:9876"}
	r := NewCustomResolver(addrs)
	addrs[0] = "changed"
	got := r.Resolve()
	want := []string{"127.0.0.1:9876", "[::1]:9876"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses=%v", got)
	}
	got[0] = "changed again"
	if !reflect.DeepEqual(r.Resolve(), want) {
		t.Fatal("Resolve exposes owned addresses")
	}
}
