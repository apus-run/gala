package nsq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/apus-run/gala/components/mq"
	nsqclient "github.com/nsqio/go-nsq"
)

var (
	// ErrClosed is returned by operations on a closed client
	// | ErrClosed 由已关闭客户端上的操作返回。
	ErrClosed = errors.New("nsq: client closed")
	// ErrBusy is returned when another lifecycle call is still in progress
	// | ErrBusy 表示另一个生命周期调用正在进行。
	ErrBusy = errors.New("nsq: lifecycle operation in progress")
	// ErrBackpressure is returned when too many sends are still pending
	// | ErrBackpressure 表示未完成的发送过多。
	ErrBackpressure = errors.New("nsq: too many pending sends")
)

// Factory creates NSQ producers and consumers | Factory 创建 NSQ 生产者与消费者。
type Factory struct{}

// NewFactory creates a Factory | NewFactory 创建 Factory。
func NewFactory() mq.Factory { return &Factory{} }

func validateAddresses(addrs []string) error {
	if len(addrs) == 0 {
		return errors.New("nsq: addr is empty")
	}
	for _, addr := range addrs {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("nsq: invalid nsqd TCP address %q", addr)
		}
	}
	return nil
}

// NewProducer creates a Producer for cfg, one go-nsq client per address.
// Publish retries and compression other than none are unsupported.
// NewProducer 依据 cfg 创建 Producer，每个地址一个 go-nsq 客户端。
// 发布重试与非空压缩不支持。
func (f *Factory) NewProducer(cfg mq.ProducerConfig) (mq.Producer, error) {
	if err := validateAddresses(cfg.Addr); err != nil {
		return nil, err
	}
	if cfg.RetryTimes < 0 || cfg.ProduceTimeout < 0 {
		return nil, errors.New("nsq: negative retry count or timeout")
	}
	if cfg.RetryTimes != 0 || cfg.Compression != mq.CompressionNone {
		return nil, fmt.Errorf("nsq: publish retries or compression: %w", mq.ErrUnsupported)
	}
	config := nsqclient.NewConfig()
	if cfg.ProduceTimeout > 0 {
		config.DialTimeout, config.ReadTimeout, config.WriteTimeout = cfg.ProduceTimeout, cfg.ProduceTimeout, cfg.ProduceTimeout
		config.HeartbeatInterval = min(config.HeartbeatInterval, cfg.ProduceTimeout/2)
		// nsqd requires heartbeats >= 1s; -1ms disables them on the wire.
		// Short producer I/O deadlines then close idle sockets; go-nsq reconnects.
		// nsqd 要求心跳 >= 1 秒；-1ms 在协议上关闭心跳。较短的生产者 I/O
		// 超时会随后关闭空闲连接，go-nsq 会自行重连。
		if config.HeartbeatInterval < time.Second {
			config.HeartbeatInterval = -time.Millisecond
		}
	}
	if cfg.ProducerGroup != nil {
		config.ClientID = *cfg.ProducerGroup
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	p := &Producer{slots: make(chan struct{}, maxPendingSends), stop: make(chan struct{})}
	for _, addr := range cfg.Addr {
		client, err := nsqclient.NewProducer(addr, config)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.clients = append(p.clients, client)
	}
	return p, nil
}

func consumerConfig(cfg mq.ConsumerConfig) (*nsqclient.Config, error) {
	if err := validateAddresses(cfg.Addr); err != nil {
		return nil, err
	}
	if !nsqclient.IsValidTopicName(cfg.Topic) || !nsqclient.IsValidChannelName(cfg.ConsumerGroup) {
		return nil, errors.New("nsq: invalid topic or consumer group (channel)")
	}
	if cfg.Orderly || cfg.TagExpression != "" {
		return nil, fmt.Errorf("nsq: orderly consumption or tag filtering: %w", mq.ErrUnsupported)
	}
	if cfg.ConsumeTimeout < 0 || cfg.ConsumeGoroutineNums < 0 {
		return nil, errors.New("nsq: negative timeout or concurrency")
	}
	config := nsqclient.NewConfig()
	// The SDK's default MaxAttempts=5 would FIN a repeatedly failing message.
	// SDK 默认的 MaxAttempts=5 会把反复失败的消息 FIN 掉。
	config.MaxAttempts = 0
	config.MaxInFlight = max(1, cfg.ConsumeGoroutineNums)
	config.MsgTimeout = cfg.ConsumeTimeout
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

// NewConsumer creates a Consumer for cfg; ConsumerGroup becomes the NSQ
// channel name.
// NewConsumer 依据 cfg 创建 Consumer；ConsumerGroup 作为 NSQ 的 channel 名。
func (f *Factory) NewConsumer(cfg mq.ConsumerConfig) (mq.Consumer, error) {
	config, err := consumerConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := nsqclient.NewConsumer(cfg.Topic, cfg.ConsumerGroup, config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Consumer{client: client, stop: client.StopChan, cancel: cancel,
		addrs: append([]string(nil), cfg.Addr...), topic: cfg.Topic, timeout: cfg.ConsumeTimeout}
	// Register before exposing c: StopChan only closes after an SDK handler
	// exits, even if Close is called before Start or Start rejects a missing
	// handler.
	// 在暴露 c 之前先注册：只有 SDK 处理器退出后 StopChan 才会关闭，
	// 即使 Close 在 Start 之前调用、或 Start 因缺少处理器而拒绝。
	client.AddConcurrentHandlers(nsqclient.HandlerFunc(func(message *nsqclient.Message) error {
		return c.handle(ctx, message)
	}), config.MaxInFlight)
	return c, nil
}
