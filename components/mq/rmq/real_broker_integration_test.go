//go:build integration && realbroker

package rmq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rocketadmin "github.com/apache/rocketmq-client-go/v2/admin"
	"github.com/apus-run/gala/components/mq"
)

// Keep the real-broker check in its own process because RocketMQ's Go SDK
// caches one nameserver address globally for the lifetime of a process.
func TestRocketMQRealBrokerEndToEnd(t *testing.T) {
	if os.Getenv("ROCKETMQ_NAMESRV_ADDR") == "" {
		t.Skip("set ROCKETMQ_NAMESRV_ADDR to a disposable RocketMQ nameserver")
	}
	if os.Getenv("ROCKETMQ_BROKER_ADDR") == "" {
		t.Skip("set ROCKETMQ_BROKER_ADDR to a disposable RocketMQ broker")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRocketMQRealBrokerSubprocess$")
	cmd.Env = append(os.Environ(), "ROCKETMQ_REAL_BROKER_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real RocketMQ subprocess failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestRocketMQRealBrokerSubprocess(t *testing.T) {
	if os.Getenv("ROCKETMQ_REAL_BROKER_CHILD") != "1" {
		t.Skip("runs only in isolated real-broker subprocess")
	}
	runRocketMQRealBrokerEndToEnd(t)
}

// Exercises send modes, metadata, delay delivery, consumer acknowledgements,
// and redelivery against a disposable RocketMQ broker.
func runRocketMQRealBrokerEndToEnd(t *testing.T) {
	addrs := strings.Split(os.Getenv("ROCKETMQ_NAMESRV_ADDR"), ",")
	topic := fmt.Sprintf("gala_mq_%d", time.Now().UnixNano())
	group := topic + "_group"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	producer, err := NewFactory().NewProducer(mq.ProducerConfig{Addr: addrs, RetryTimes: 2, ProduceTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if err := producer.Start(); err != nil {
		t.Fatal(err)
	}
	// Allow the single-node broker to finish publishing its TBW102 route.
	time.Sleep(2 * time.Second)
	// Publishing once makes the broker publish the new topic route to the
	// nameserver before the consumer subscribes.
	if response, err := producer.Send(ctx, mq.NewMessage(topic, []byte("bootstrap"))); err != nil || response.MessageID == "" {
		t.Fatalf("bootstrap Send response=%+v error=%v", response, err)
	}
	admin, err := rocketadmin.NewAdmin(rocketadmin.WithResolver(NewCustomResolver(addrs)))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.CreateTopic(ctx,
		rocketadmin.WithTopicCreate("%RETRY%"+group),
		rocketadmin.WithBrokerAddrCreate(os.Getenv("ROCKETMQ_BROKER_ADDR")),
	); err != nil {
		t.Fatalf("create retry topic: %v", err)
	}

	consumer, err := NewFactory().NewConsumer(mq.ConsumerConfig{Addr: addrs, Topic: topic, ConsumerGroup: group, ConsumeGoroutineNums: 1, ConsumeTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	counts := make(map[string]int)
	var retried atomic.Bool
	delivered := make(chan struct{}, 8)
	bootstrapConsumed := make(chan struct{}, 1)
	retryRedelivered := make(chan struct{}, 1)
	badMessage := make(chan error, 1)
	consumer.RegisterHandler(handlerFunc(func(_ context.Context, message *mq.MessageExt) error {
		body := string(message.Body)
		if message.MsgID == "" || (message.Topic != topic && body != "retry") {
			select {
			case badMessage <- fmt.Errorf("invalid RocketMQ message metadata: %+v", message):
			default:
			}
		}
		if body == "tagged" && (message.Tag != "created" || message.PartitionKey != "account-1" || message.Properties["source"] != "real-broker") {
			select {
			case badMessage <- fmt.Errorf("RocketMQ metadata lost: %+v", message):
			default:
			}
		}
		mu.Lock()
		counts[body]++
		mu.Unlock()
		if body == "bootstrap" {
			bootstrapConsumed <- struct{}{}
			delivered <- struct{}{}
			return nil
		}
		if body == "retry" && retried.CompareAndSwap(false, true) {
			return errors.New("exercise broker redelivery after handler error")
		}
		if body == "retry" {
			retryRedelivered <- struct{}{}
		}
		delivered <- struct{}{}
		return nil
	}))
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	select {
	case <-bootstrapConsumed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	ordered := mq.NewOrderlyMessage(topic, "account-1", []byte("tagged")).WithTag("created").WithProperties(map[string]string{"source": "real-broker"})
	if response, err := producer.Send(ctx, ordered); err != nil || response.MessageID == "" {
		t.Fatalf("Send response=%+v error=%v", response, err)
	}
	if response, err := producer.Send(ctx, mq.NewMessage(topic, []byte("retry"))); err != nil || response.MessageID == "" {
		t.Fatalf("retry Send response=%+v error=%v", response, err)
	}
	if response, err := producer.SendBatch(ctx, []*mq.Message{mq.NewMessage(topic, []byte("batch-1")), mq.NewMessage(topic, []byte("batch-2"))}); err != nil {
		t.Fatalf("SendBatch response=%+v error=%v", response, err)
	}
	async := make(chan error, 1)
	if err := producer.SendAsync(ctx, func(_ context.Context, response mq.SendResponse, err error) {
		if err == nil && response.MessageID == "" {
			err = errors.New("RocketMQ async response has no message ID")
		}
		async <- err
	}, mq.NewMessage(topic, []byte("async"))); err != nil {
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
	// The SDK maps this to its first delay level (one second).
	if response, err := producer.Send(ctx, mq.NewDeferMessage(topic, time.Nanosecond, []byte("delayed"))); err != nil || response.MessageID == "" {
		t.Fatalf("delayed Send response=%+v error=%v", response, err)
	}
	for range 7 { // the bootstrap and successful deliveries are acknowledged.
		select {
		case <-delivered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case <-retryRedelivered:
	case <-ctx.Done():
		t.Fatal("RocketMQ did not redeliver failed message: ", ctx.Err())
	}
	select {
	case err := <-badMessage:
		t.Fatal(err)
	default:
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["bootstrap"] != 1 || counts["tagged"] != 1 || counts["retry"] != 2 || counts["batch-1"] != 1 || counts["batch-2"] != 1 || counts["async"] != 1 || counts["delayed"] != 1 {
		t.Fatalf("delivery counts=%v", counts)
	}
}
