package mq

import (
	"errors"
	"time"
)

// ErrUnsupported indicates that a backend or operation is not implemented.
// ErrUnsupported 表示后端或操作未实现。
var ErrUnsupported = errors.New("mq: unsupported operation")

// Factory creates producers and consumers for one backend | Factory 为一个后端创建生产者与消费者.
//
//go:generate mockgen -destination=mocks/factory.go -package=mocks . Factory
type Factory interface {
	// NewProducer builds a Producer from cfg | NewProducer 依据 cfg 构建 Producer.
	NewProducer(ProducerConfig) (Producer, error)
	// NewConsumer builds a Consumer from cfg | NewConsumer 依据 cfg 构建 Consumer.
	NewConsumer(ConsumerConfig) (Consumer, error)
}

// ProducerConfig configures a producer | ProducerConfig 配置生产者.
type ProducerConfig struct {
	// Name server address | 名称服务器地址
	Addr []string
	// Timeout for producing one message | 单条消息的生产超时
	ProduceTimeout time.Duration
	// Retry times for producing | 生产重试次数
	RetryTimes int
	// Use compression, default is no compression | 是否压缩，默认不压缩
	Compression CompressionCodec
	// Producer group name | 生产者组名
	ProducerGroup *string
}

// ConsumerConfig configures a consumer | ConsumerConfig 配置消费者.
type ConsumerConfig struct {
	// Name server address | 名称服务器地址
	Addr []string
	// Topic name | 主题名
	Topic string
	// Consumer group name | 消费者组名
	ConsumerGroup string
	// Whether to consume orderly | 是否顺序消费
	Orderly bool
	// Consume specific tags, such as "tag" or "tag1 || tag2 || tag3" | 只消费特定标签，如 "tag" 或 "tag1 || tag2 || tag3"
	TagExpression string
	// Max number of messages consumed concurrently | 最大并发消费消息数
	ConsumeGoroutineNums int
	// Timeout for consumer one message | 单条消息的消费超时
	ConsumeTimeout time.Duration
}

// CompressionCodec selects a compression algorithm | CompressionCodec 选择压缩算法.
type CompressionCodec int

const (
	// CompressionNone no compression | 不压缩
	CompressionNone CompressionCodec = iota
	// CompressionZSTD compression using ZSTD | 使用 ZSTD 压缩
	CompressionZSTD
)
