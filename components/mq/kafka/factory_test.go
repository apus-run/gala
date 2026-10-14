package kafka

import (
	"errors"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
)

func TestFactoryRejectsUnsupportedConfig(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         mq.ConsumerConfig
		unsupported bool
	}{
		{"missing", mq.ConsumerConfig{}, false},
		{"negative_timeout", mq.ConsumerConfig{Addr: []string{"127.0.0.1:1"}, Topic: "events", ConsumerGroup: "test", ConsumeTimeout: -time.Second}, false},
		{"concurrency", mq.ConsumerConfig{Addr: []string{"127.0.0.1:1"}, Topic: "events", ConsumerGroup: "test", ConsumeGoroutineNums: 2}, true},
		{"tag_filter", mq.ConsumerConfig{Addr: []string{"127.0.0.1:1"}, Topic: "events", ConsumerGroup: "test", TagExpression: "tag"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewFactory().NewConsumer(tc.cfg)
			if c != nil || err == nil {
				t.Fatalf("consumer=%v err=%v", c, err)
			}
			if tc.unsupported && !errors.Is(err, mq.ErrUnsupported) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		cfg  mq.ProducerConfig
	}{
		{"missing", mq.ProducerConfig{}},
		{"negative_retries", mq.ProducerConfig{Addr: []string{"127.0.0.1:1"}, RetryTimes: -1}},
		{"compression", mq.ProducerConfig{Addr: []string{"127.0.0.1:1"}, Compression: mq.CompressionCodec(42)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewFactory().NewProducer(tc.cfg)
			if p != nil || err == nil {
				t.Fatalf("producer=%v err=%v", p, err)
			}
		})
	}
}
