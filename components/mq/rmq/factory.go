package rmq

import (
	"errors"
	"fmt"
	"net"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"

	"github.com/apus-run/gala/components/mq"
)

// Factory creates RocketMQ producers and consumers | Factory 创建 RocketMQ 生产者与消费者。
type Factory struct{}

// NewFactory creates a Factory | NewFactory 创建 Factory。
func NewFactory() mq.Factory {
	return &Factory{}
}

// NewProducer creates a Producer for config | NewProducer 依据 config 创建 Producer。
func (f *Factory) NewProducer(config mq.ProducerConfig) (mq.Producer, error) {
	opts, err := producerOptions(config)
	if err != nil {
		return nil, err
	}
	p, err := rocketmq.NewProducer(opts...)
	if err != nil {
		return nil, err
	}
	return &Producer{producer: p}, nil
}

func producerOptions(config mq.ProducerConfig) ([]producer.Option, error) {
	if len(config.Addr) == 0 {
		return nil, errors.New("addr is empty")
	}
	if config.RetryTimes < 0 || config.ProduceTimeout < 0 {
		return nil, errors.New("negative retry count or produce timeout")
	}
	opts := []producer.Option{
		producer.WithNsResolver(NewCustomResolver(config.Addr)),
		producer.WithRetry(config.RetryTimes),
		producer.WithQueueSelector(producer.NewHashQueueSelector()),
	}
	if config.ProduceTimeout > 0 {
		opts = append(opts, producer.WithSendMsgTimeout(config.ProduceTimeout))
	}
	if config.Compression != mq.CompressionNone {
		return nil, errors.New("compression not supported")
	}
	if config.ProducerGroup != nil {
		opts = append(opts, producer.WithGroupName(*config.ProducerGroup))
	}

	return opts, nil
}

// NewConsumer creates a push Consumer for config | NewConsumer 依据 config 创建 push 消费者。
func (f *Factory) NewConsumer(config mq.ConsumerConfig) (mq.Consumer, error) {
	if len(config.Addr) == 0 {
		return nil, errors.New("addr is empty")
	}
	if config.Topic == "" {
		return nil, errors.New("topic is empty")
	}
	if config.ConsumerGroup == "" {
		return nil, errors.New("consumer group is empty")
	}

	opts := []consumer.Option{
		consumer.WithNsResolver(NewCustomResolver(config.Addr)),
		consumer.WithGroupName(config.ConsumerGroup),
		consumer.WithConsumerOrder(config.Orderly),
	}
	if config.ConsumeGoroutineNums > 0 {
		opts = append(opts, consumer.WithConsumeGoroutineNums(config.ConsumeGoroutineNums))
	}
	if config.ConsumeTimeout > 0 {
		opts = append(opts, consumer.WithConsumeTimeout(config.ConsumeTimeout))
	}
	var selector consumer.MessageSelector
	if config.TagExpression != "" {
		selector = consumer.MessageSelector{
			Type:       consumer.TAG,
			Expression: config.TagExpression,
		}
	}

	c, err := rocketmq.NewPushConsumer(opts...)
	if err != nil {
		return nil, err
	}
	return &Consumer{
		consumer: c,
		topic:    config.Topic,
		selector: selector,
	}, nil
}

// NewCustomResolver returns a name-server resolver over addrs
// | NewCustomResolver 基于 addrs 返回 name server resolver。
func NewCustomResolver(addrs []string) primitive.NsResolver {
	return &customResolver{addrs: append([]string(nil), addrs...)}
}

type customResolver struct {
	addrs []string
}

// Resolve implements primitive.NsResolver, resolving hostnames to IPs | 实现 primitive.NsResolver，把主机名解析为 IP.
func (c *customResolver) Resolve() []string {
	ret := make([]string, len(c.addrs))
	for i, addr := range c.addrs {
		ret[i] = addr
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		if net.ParseIP(host) != nil {
			continue
		}
		addrs, _ := net.LookupHost(host)
		if len(addrs) > 0 {
			ret[i] = net.JoinHostPort(addrs[0], port)
		}
	}
	return ret
}

// Description implements primitive.NsResolver | 实现 primitive.NsResolver.
func (c *customResolver) Description() string {
	return fmt.Sprintf("custom resolver: %v", c.addrs)
}
