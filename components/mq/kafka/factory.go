package kafka

import (
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/apus-run/gala/components/mq"
)

// Factory builds kafka producers and consumers on franz-go. Its options
// (WithMaxRetries, WithDeadLetterTopic) shape the retry behavior of the
// consumers it creates.
// Factory 基于 franz-go 构建 kafka 生产者与消费者。
// 其选项（WithMaxRetries、WithDeadLetterTopic）决定所创建消费者的重试行为。
type Factory struct {
	maxRetries int
	deadLetter string
}

// Option configures a Factory | Option 配置 Factory。
type Option interface {
	apply(*Factory)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Factory)

func (f OptionFunc) apply(o *Factory) {
	f(o)
}

// WithMaxRetries caps redeliveries of a failing record (after the first
// attempt). When exhausted the record is forwarded to the dead-letter topic,
// or logged and skipped if none is set, so it no longer blocks its partition.
// n <= 0 keeps the default: retry forever.
// WithMaxRetries 限制失败记录的重投次数（不含首次尝试）。耗尽后记录被转入
// 死信 topic；未配置死信则记录日志后跳过，不再阻塞所在分区。
// n <= 0 保持默认：永远重试。
func WithMaxRetries(n int) Option { return OptionFunc(func(f *Factory) { f.maxRetries = n }) }

// WithDeadLetterTopic sets where exhausted records go. The offset is committed
// only after the forward succeeds, so a failed forward retries instead of
// losing it.
// WithDeadLetterTopic 设置重试耗尽后记录的去向。转发成功后才提交位移，
// 转发失败则重试转发，不会丢消息。
func WithDeadLetterTopic(topic string) Option {
	return OptionFunc(func(f *Factory) { f.deadLetter = topic })
}

// NewFactory creates a Factory from opts | NewFactory 按选项创建 Factory。
func NewFactory(opts ...Option) mq.Factory {
	f := &Factory{}
	for _, opt := range opts {
		opt.apply(f)
	}
	return f
}

// NewProducer creates a Producer for cfg. It requires a non-empty Addr,
// a non-negative retry count and timeout, and none or ZSTD compression.
// NewProducer 依据 cfg 创建 Producer。要求 Addr 非空、重试次数与超时非负、
// 压缩为不压缩或 ZSTD。
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

// NewConsumer creates a Consumer for cfg. It requires Addr, Topic and
// ConsumerGroup; a zero ConsumeTimeout defaults to 30 seconds.
// NewConsumer 依据 cfg 创建 Consumer。要求 Addr、Topic 与 ConsumerGroup；
// ConsumeTimeout 为 0 时默认 30 秒。
func (f *Factory) NewConsumer(cfg mq.ConsumerConfig) (mq.Consumer, error) {
	if len(cfg.Addr) == 0 || cfg.Topic == "" || cfg.ConsumerGroup == "" {
		return nil, errors.New("kafka: addr, topic and consumer group are required")
	}
	if cfg.ConsumeTimeout < 0 || cfg.ConsumeGoroutineNums < 0 {
		return nil, errors.New("kafka: negative consume timeout or concurrency")
	}
	// Serial processing keeps commit ordering explicit; scale with group members.
	// 串行处理让提交顺序保持显式；需要扩容时增加消费组成员。
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
