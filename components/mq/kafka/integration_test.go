//go:build integration

package kafka

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// This uses real Kafka protocol requests against an in-process broker, without Docker.
func TestKafkaProtocolRetryAndCommit(t *testing.T) {
	cluster := kfake.MustCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, "events"))
	defer cluster.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	factory := NewFactory()
	p, err := factory.NewProducer(mq.ProducerConfig{Addr: cluster.ListenAddrs(), Compression: mq.CompressionZSTD, RetryTimes: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	messages := []*mq.Message{
		mq.NewOrderlyMessage("events", "account", []byte("first")).WithTag("created").WithProperties(map[string]string{"source": "test"}),
		mq.NewOrderlyMessage("events", "account", []byte("second")),
	}
	if _, err := p.SendBatch(ctx, messages); err != nil {
		t.Fatal(err)
	}
	asyncDone := make(chan error, 1)
	if err := p.SendAsync(ctx, func(_ context.Context, response mq.SendResponse, err error) {
		if err == nil && response.MessageID == "" {
			err = errors.New("empty async message ID")
		}
		asyncDone <- err
	}, mq.NewOrderlyMessage("events", "account", []byte("third"))); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-asyncDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	var mu sync.Mutex
	var offsets []int64
	committed := make(chan struct{}, 1)
	cluster.ControlKey(8, func(req kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		commit := req.(*kmsg.OffsetCommitRequest)
		mu.Lock()
		defer mu.Unlock()
		for _, topic := range commit.Topics {
			for _, partition := range topic.Partitions {
				offsets = append(offsets, partition.Offset)
				if partition.Offset == 3 {
					select {
					case committed <- struct{}{}:
					default:
					}
				}
			}
		}
		return nil, nil, false
	})
	consumer, err := factory.NewConsumer(mq.ConsumerConfig{Addr: cluster.ListenAddrs(), Topic: "events", ConsumerGroup: "workers", ConsumeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	var attempts []string
	consumer.RegisterHandler(handlerFunc(func(_ context.Context, m *mq.MessageExt) error {
		attempts = append(attempts, string(m.Body))
		if string(m.Body) == "first" {
			if m.Tag != "created" || m.PartitionKey != "account" || m.Properties["source"] != "test" {
				return errors.New("metadata lost")
			}
			if len(attempts) == 1 {
				panic("transient handler panic")
			}
		}
		return nil
	}))
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attempts, []string{"first", "first", "second", "third"}) {
		t.Fatalf("attempts=%v", attempts)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(offsets, []int64{1, 2, 3}) {
		t.Fatalf("committed offsets=%v", offsets)
	}
}
