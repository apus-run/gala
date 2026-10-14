package nsq

import (
	"errors"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
)

func TestFactoryValidation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         mq.ProducerConfig
		unsupported bool
	}{
		{"empty", mq.ProducerConfig{}, false},
		{"bad_address", mq.ProducerConfig{Addr: []string{"localhost"}}, false},
		{"negative_timeout", mq.ProducerConfig{Addr: []string{"127.0.0.1:4150"}, ProduceTimeout: -1}, false},
		{"retry", mq.ProducerConfig{Addr: []string{"127.0.0.1:4150"}, RetryTimes: 1}, true},
		{"compression", mq.ProducerConfig{Addr: []string{"127.0.0.1:4150"}, Compression: mq.CompressionZSTD}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewFactory().NewProducer(tc.cfg)
			if err == nil || p != nil {
				t.Fatalf("producer=%v error=%v", p, err)
			}
			if tc.unsupported && !errors.Is(err, mq.ErrUnsupported) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	base := mq.ConsumerConfig{Addr: []string{"127.0.0.1:4150"}, Topic: "events", ConsumerGroup: "workers"}
	for _, tc := range []struct {
		name        string
		modify      func(*mq.ConsumerConfig)
		unsupported bool
	}{
		{"empty_topic", func(c *mq.ConsumerConfig) { c.Topic = "" }, false},
		{"empty_channel", func(c *mq.ConsumerConfig) { c.ConsumerGroup = "" }, false},
		{"orderly", func(c *mq.ConsumerConfig) { c.Orderly = true }, true},
		{"tag", func(c *mq.ConsumerConfig) { c.TagExpression = "tag" }, true},
		{"negative_concurrency", func(c *mq.ConsumerConfig) { c.ConsumeGoroutineNums = -1 }, false},
		{"negative_timeout", func(c *mq.ConsumerConfig) { c.ConsumeTimeout = -1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.modify(&cfg)
			c, err := NewFactory().NewConsumer(cfg)
			if err == nil || c != nil {
				t.Fatalf("consumer=%v error=%v", c, err)
			}
			if tc.unsupported && !errors.Is(err, mq.ErrUnsupported) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestConsumerNeverDiscardsAfterAttempts(t *testing.T) {
	cfg, err := consumerConfig(mq.ConsumerConfig{Addr: []string{"127.0.0.1:4150"}, Topic: "events", ConsumerGroup: "workers", ConsumeGoroutineNums: 4, ConsumeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxAttempts != 0 {
		t.Fatalf("messages will be discarded after %d attempts", cfg.MaxAttempts)
	}
	if cfg.MaxInFlight != 4 || cfg.MsgTimeout != time.Second {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestRealClientsCloseBeforeStart(t *testing.T) {
	c, err := NewFactory().NewConsumer(mq.ConsumerConfig{Addr: []string{"127.0.0.1:4150"}, Topic: "events", ConsumerGroup: "workers"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err == nil {
		t.Fatal("missing handler accepted")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := NewFactory().NewProducer(mq.ProducerConfig{Addr: []string{"127.0.0.1:4150"}, ProduceTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close: %v", err)
	}
}
