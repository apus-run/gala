//go:build integration

package nsq

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	nsqclient "github.com/nsqio/go-nsq"
)

// Run against a disposable nsqd with --max-req-timeout=100ms to bound retry time.
func TestNSQDProtocol(t *testing.T) {
	addr := os.Getenv("NSQD_TCP_ADDR")
	if addr == "" {
		t.Skip("set NSQD_TCP_ADDR to a disposable nsqd (--max-req-timeout=100ms)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	topic := fmt.Sprintf("gala_test_%d", time.Now().UnixNano())
	consumer, err := NewFactory().NewConsumer(mq.ConsumerConfig{Addr: []string{addr}, Topic: topic, ConsumerGroup: "workers", ConsumeGoroutineNums: 3, ConsumeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	var mu sync.Mutex
	counts := make(map[string]int)
	var deferredAt time.Time
	consumer.RegisterHandler(handlerFunc(func(_ context.Context, message *mq.MessageExt) error {
		mu.Lock()
		defer mu.Unlock()
		body := string(message.Body)
		counts[body]++
		if len(message.MsgID) != 16 || message.Topic != topic {
			t.Errorf("message metadata=%+v", message)
		}
		if body == "retry" && counts[body] == 1 {
			panic("transient handler failure")
		}
		if body == "deferred" {
			deferredAt = time.Now()
		}
		return nil
	}))
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	p, err := NewFactory().NewProducer(mq.ProducerConfig{Addr: []string{addr}, ProduceTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	response, err := p.SendBatch(ctx, []*mq.Message{mq.NewMessage(topic, []byte("retry")), mq.NewMessage(topic, []byte("batch"))})
	if err != nil {
		t.Fatal(err)
	}
	if response != (mq.SendResponse{}) {
		t.Fatalf("NSQ invented a publish ID: %+v", response)
	}
	callback := make(chan error, 1)
	if err := p.SendAsync(ctx, func(_ context.Context, _ mq.SendResponse, err error) { callback <- err }, mq.NewMessage(topic, []byte("async"))); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-callback:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	before := time.Now()
	if _, err := p.Send(ctx, mq.NewDeferMessage(topic, 25*time.Millisecond+time.Nanosecond, []byte("deferred"))); err != nil {
		t.Fatal(err)
	}
	client := consumer.(*Consumer).client.(*nsqclient.Consumer)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		stats := client.Stats()
		if stats.MessagesFinished == 4 && stats.MessagesRequeued == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for FIN/REQ: stats=%+v: %v", stats, ctx.Err())
		case <-ticker.C:
		}
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["retry"] != 2 || counts["batch"] != 1 || counts["async"] != 1 || counts["deferred"] != 1 {
		t.Fatalf("delivery counts=%v", counts)
	}
	if deferredAt.Sub(before) < 25*time.Millisecond {
		t.Fatalf("deferred message arrived early: %v", deferredAt.Sub(before))
	}
}
