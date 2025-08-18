package mq

import (
	"time"
)

//go:generate mockgen -destination=mocks/factory.go -package=mocks . Factory
type Factory interface {
	NewProducer(ProducerConfig) (Producer, error)
	NewConsumer(ConsumerConfig) (Consumer, error)
}

type ProducerConfig struct {
	// Name server address
	Addr []string
	// Timeout for producing one message
	ProduceTimeout time.Duration
	// Retry times for producing
	RetryTimes int
	// Use compression, default is no compression
	Compression CompressionCodec
	// Producer group name
	ProducerGroup *string
}

type ConsumerConfig struct {
	// Name server address
	Addr []string
	// Topic name
	Topic string
	// Consumer group name
	ConsumerGroup string
	// Whether to consume orderly
	Orderly bool
	// Consume specific tags, such as "tag" or "tag1 || tag2 || tag3"
	TagExpression string
	// Max number of messages consumed concurrently
	ConsumeGoroutineNums int
	// Timeout for consumer one message
	ConsumeTimeout time.Duration
}

type CompressionCodec int

const (
	// CompressionNone no compression
	CompressionNone CompressionCodec = iota
	// CompressionZSTD compression using ZSTD
	CompressionZSTD
)
