//go:build integration

package kafka

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Exercises the adapter against an actual Kafka broker. Set KAFKA_BROKERS to
// a disposable broker; the protocol test in integration_test.go uses kfake.
func TestKafkaRealBrokerEndToEnd(t *testing.T) {
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if brokers[0] == "" {
		t.Skip("set KAFKA_BROKERS to a disposable Kafka broker")
	}
	topic := fmt.Sprintf("gala_mq_%d", time.Now().UnixNano())
	group := topic + "_group"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	createTopic := kmsg.NewCreateTopicsRequest()
	createTopic.TimeoutMillis = 15_000
	createTopic.Topics = []kmsg.CreateTopicsRequestTopic{{Topic: topic, NumPartitions: 3, ReplicationFactor: 1}}
	created, err := createTopic.RequestWith(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Topics) != 1 || created.Topics[0].ErrorCode != 0 {
		t.Fatalf("create topic response=%+v", created.Topics)
	}

	consumer, err := NewFactory().NewConsumer(mq.ConsumerConfig{Addr: brokers, Topic: topic, ConsumerGroup: group, ConsumeTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	counts := make(map[string]int)
	var retries atomic.Int32
	delivered := make(chan struct{}, 8)
	badMessage := make(chan error, 1)
	consumer.RegisterHandler(handlerFunc(func(_ context.Context, message *mq.MessageExt) error {
		if message.Topic != topic || message.MsgID == "" {
			select {
			case badMessage <- fmt.Errorf("invalid Kafka message metadata: %+v", message):
			default:
			}
		}
		if message.Tag == "created" && message.Properties["source"] != "real-broker" {
			select {
			case badMessage <- errors.New("Kafka tag or property metadata lost"):
			default:
			}
		}
		body := string(message.Body)
		mu.Lock()
		counts[body]++
		mu.Unlock()
		if body == "retry" && retries.CompareAndSwap(0, 1) {
			panic("exercise broker redelivery after handler panic")
		}
		delivered <- struct{}{}
		return nil
	}))
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	// Let the consumer join its group before publishing; the topic is created
	// by Kafka's normal auto-create behavior on this disposable test broker.
	time.Sleep(time.Second)

	producer, err := NewFactory().NewProducer(mq.ProducerConfig{Addr: brokers, Compression: mq.CompressionZSTD, RetryTimes: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if err := producer.Start(); err != nil {
		t.Fatal(err)
	}
	ordered := func(body string) *mq.Message { return mq.NewOrderlyMessage(topic, "account-1", []byte(body)) }
	if response, err := producer.Send(ctx, ordered("retry").WithTag("created").WithProperties(map[string]string{"source": "real-broker"})); err != nil || response.MessageID == "" {
		t.Fatalf("Send response=%+v error=%v", response, err)
	}
	if response, err := producer.SendBatch(ctx, []*mq.Message{ordered("batch-1"), ordered("batch-2")}); err != nil || response.MessageID == "" {
		t.Fatalf("SendBatch response=%+v error=%v", response, err)
	}
	async := make(chan error, 1)
	if err := producer.SendAsync(ctx, func(_ context.Context, response mq.SendResponse, err error) {
		if err == nil && response.MessageID == "" {
			err = errors.New("Kafka async response has no message ID")
		}
		async <- err
	}, ordered("async")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-async:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for range 4 { // the failed attempt is not acknowledged; four deliveries succeed.
		select {
		case <-delivered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case err := <-badMessage:
		t.Fatal(err)
	default:
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["retry"] != 2 || counts["batch-1"] != 1 || counts["batch-2"] != 1 || counts["async"] != 1 {
		t.Fatalf("delivery counts=%v", counts)
	}
}
