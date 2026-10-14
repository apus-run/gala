package kafka

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
)

type fakeProducer struct {
	records  []*kgo.Record
	callback func(*kgo.Record, error)
	err      error
	events   []string
}

func (f *fakeProducer) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	f.records = rs
	var results kgo.ProduceResults
	for i, r := range rs {
		r.Offset = int64(i + 10)
		r.Partition = 2
		results = append(results, kgo.ProduceResult{Record: r, Err: f.err})
	}
	return results
}
func (f *fakeProducer) TryProduce(_ context.Context, r *kgo.Record, callback func(*kgo.Record, error)) {
	f.records = []*kgo.Record{r}
	f.callback = callback
}
func (f *fakeProducer) Flush(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded flush")
	}
	f.events = append(f.events, "flush")
	return f.err
}
func (f *fakeProducer) Close() { f.events = append(f.events, "close") }

func TestProducerSend(t *testing.T) {
	client := &fakeProducer{}
	p := &Producer{client: client}
	m := mq.NewOrderlyMessage("events", "key", []byte("body")).WithTag("tag").WithProperties(map[string]string{"user": "value"})
	resp, err := p.Send(context.Background(), m)
	if err != nil || resp.MessageID != "events:2:10" || resp.Offset != 10 {
		t.Fatalf("response=%+v error=%v", resp, err)
	}
	got := fromRecord(client.records[0])
	if !reflect.DeepEqual(got.Message, *m) {
		t.Fatalf("round trip=%+v", got.Message)
	}
	m.Body[0] = 'X'
	m.Properties["user"] = "changed"
	if string(client.records[0].Value) != "body" || fromRecord(client.records[0]).Properties["user"] != "value" {
		t.Fatal("input mutation reached SDK")
	}
	got.Body[0] = 'Y'
	if string(client.records[0].Value) != "body" {
		t.Fatal("handler mutation reached record")
	}
	resp, err = p.SendBatch(context.Background(), []*mq.Message{m, m})
	if err != nil || resp.MessageID != "events:2:11" {
		t.Fatalf("batch response=%+v error=%v", resp, err)
	}
	client.err = errors.New("delivery failed")
	if _, err := p.Send(context.Background(), m); !errors.Is(err, client.err) {
		t.Fatalf("delivery error=%v", err)
	}
}

func TestProducerAsyncAndClose(t *testing.T) {
	client := &fakeProducer{}
	p := &Producer{client: client}
	ctx := context.Background()
	calls := 0
	failure := errors.New("delivery failed")
	if err := p.SendAsync(ctx, func(got context.Context, resp mq.SendResponse, err error) {
		calls++
		if got != ctx || !errors.Is(err, failure) || resp != (mq.SendResponse{}) {
			t.Errorf("callback: %+v %v", resp, err)
		}
	}, mq.NewMessage("events", []byte("body"))); err != nil {
		t.Fatal(err)
	}
	client.callback(client.records[0], failure)
	if calls != 1 {
		t.Fatalf("callbacks=%d", calls)
	}
	client.err = failure
	for range 2 {
		if err := p.Close(); !errors.Is(err, failure) {
			t.Fatalf("close=%v", err)
		}
	}
	if !reflect.DeepEqual(client.events, []string{"flush", "close"}) {
		t.Fatalf("events=%v", client.events)
	}
	if err := p.Start(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := p.Send(ctx, mq.NewMessage("events", nil)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestProducerValidation(t *testing.T) {
	for _, m := range []*mq.Message{nil, {}, mq.NewDeferMessage("events", time.Second, nil), mq.NewMessage("events", nil).WithProperties(map[string]string{tagHeader: "collision"})} {
		client := &fakeProducer{}
		p := &Producer{client: client}
		if _, err := p.SendBatch(context.Background(), []*mq.Message{mq.NewMessage("events", nil), m}); err == nil {
			t.Fatal("invalid batch accepted")
		}
		if len(client.records) != 0 {
			t.Fatal("partially sent invalid batch")
		}
	}
	p := &Producer{}
	if err := p.SendAsync(context.Background(), nil, mq.NewMessage("events", nil)); err == nil {
		t.Fatal("nil callback accepted")
	}
	if _, err := p.SendBatch(context.Background(), nil); err == nil {
		t.Fatal("empty batch accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Send(ctx, mq.NewMessage("events", nil)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
