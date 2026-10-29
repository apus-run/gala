//go:build integration

package kafka

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// A poison record is dead-lettered after its retries and stops blocking the partition.
func TestKafkaRealBrokerDeadLetter(t *testing.T) {
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if brokers[0] == "" {
		t.Skip("set KAFKA_BROKERS to a disposable Kafka broker")
	}
	topic := fmt.Sprintf("gala_dlq_%d", time.Now().UnixNano())
	dlq := topic + ".dlq"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admin, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	req := kmsg.NewCreateTopicsRequest()
	req.TimeoutMillis = 15_000
	req.Topics = []kmsg.CreateTopicsRequestTopic{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}, {Topic: dlq, NumPartitions: 1, ReplicationFactor: 1}}
	if _, err := req.RequestWith(ctx, admin); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"poison", "good-1", "good-2"} {
		if err := admin.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: []byte(v)}).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var good []string
	tries := 0
	consumer, err := NewFactory(WithMaxRetries(2), WithDeadLetterTopic(dlq)).NewConsumer(mq.ConsumerConfig{
		Addr: brokers, Topic: topic, ConsumerGroup: topic + "_g", ConsumeTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	consumer.RegisterHandler(handlerFunc(func(_ context.Context, m *mq.MessageExt) error {
		mu.Lock()
		defer mu.Unlock()
		if string(m.Body) == "poison" {
			tries++
			return errors.New("cannot process")
		}
		good = append(good, string(m.Body))
		return nil
	}))
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()

	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		n := len(good)
		mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("partition blocked behind the poison record; good=%v", good)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if tries != 3 || strings.Join(good, ",") != "good-1,good-2" {
		t.Fatalf("tries=%d good=%v", tries, good)
	}

	dl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(dlq), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	recs := dl.PollRecords(ctx, 1).Records()
	if len(recs) != 1 || string(recs[0].Value) != "poison" {
		t.Fatalf("dead-letter records=%v", recs)
	}
}
