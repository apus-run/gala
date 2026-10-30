package kafka

import (
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/apus-run/gala/components/mq"
)

type Factory struct {
	maxRetries int
	deadLetter string
}

type Option interface {
	apply(*Factory)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Factory)

func (f OptionFunc) apply(o *Factory) {
	f(o)
}

// WithMaxRetries caps redeliveries of a failing record (after the first
// attempt). When exhausted the record is forwarded to the dead-letter topic,
// or logged and skipped if none is set, so it no longer blocks its partition.
// n <= 0 keeps the default: retry forever.
func WithMaxRetries(n int) Option { return OptionFunc(func(f *Factory) { f.maxRetries = n }) }

// WithDeadLetterTopic sets where exhausted records go. The offset is committed
// only after the forward succeeds, so a failed forward retries instead of losing it.
func WithDeadLetterTopic(topic string) Option {
	return OptionFunc(func(f *Factory) { f.deadLetter = topic })
}

func NewFactory(opts ...Option) mq.Factory {
	f := &Factory{}
	for _, opt := range opts {
		opt.apply(f)
	}
	return f
}

func (f *Factory) NewProducer(cfg mq.ProducerConfig) (mq.Producer, error) {
	if len(cfg.Addr) == 0 {
		return nil, errors.New("kafka: addr is empty")
	}
	if cfg.RetryTimes < 0 || cfg.ProduceTimeout < 0 {
		return nil, errors.New("kafka: negative retry count or timeout")
	}
	codec := kgo.NoCompression()
	switch cfg.Compression {
	case mq.CompressionNone:
	case mq.CompressionZSTD:
		codec = kgo.ZstdCompression()
	default:
		return nil, fmt.Errorf("kafka: compression: %w", mq.ErrUnsupported)
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Addr...),
		kgo.RecordRetries(cfg.RetryTimes),
		kgo.ProducerBatchCompression(codec),
	}
	if cfg.ProduceTimeout > 0 {
		opts = append(opts, kgo.RecordDeliveryTimeout(cfg.ProduceTimeout))
	}
	if cfg.ProducerGroup != nil {
		opts = append(opts, kgo.ClientID(*cfg.ProducerGroup))
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return &Producer{client: client}, nil
}

func (f *Factory) NewConsumer(cfg mq.ConsumerConfig) (mq.Consumer, error) {
	if len(cfg.Addr) == 0 || cfg.Topic == "" || cfg.ConsumerGroup == "" {
		return nil, errors.New("kafka: addr, topic and consumer group are required")
	}
	if cfg.ConsumeTimeout < 0 || cfg.ConsumeGoroutineNums < 0 {
		return nil, errors.New("kafka: negative consume timeout or concurrency")
	}
	// Serial processing keeps commit ordering explicit; scale with group members.
	if cfg.ConsumeGoroutineNums > 1 || cfg.TagExpression != "" {
		return nil, fmt.Errorf("kafka: concurrent handlers or tag filtering: %w", mq.ErrUnsupported)
	}
	timeout := cfg.ConsumeTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Addr...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
	)
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client, timeout: timeout, maxRetries: f.maxRetries, deadLetter: f.deadLetter}, nil
}
