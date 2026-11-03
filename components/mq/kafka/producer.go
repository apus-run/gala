package kafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ErrClosed is returned by operations on a closed producer or consumer
// | ErrClosed 由已关闭的生产者或消费者上的操作返回。
var ErrClosed = errors.New("kafka: client closed")

type producerClient interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	TryProduce(context.Context, *kgo.Record, func(*kgo.Record, error))
	Flush(context.Context) error
	Close()
}

// Producer publishes messages to Kafka through franz-go | Producer 通过 franz-go 向 Kafka 发布消息。
type Producer struct {
	client    producerClient
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

// Start is a lifecycle compatibility hook; franz-go connects lazily on use.
// Start 是生命周期兼容钩子；franz-go 在使用时才惰性建连。
func (p *Producer) Start() error {
	if p.closed.Load() {
		return ErrClosed
	}
	return nil
}

// Close flushes pending sends for up to ten seconds, then closes the client.
// Callbacks must not call Close or block on this producer.
// Close 最多用十秒刷新未完成的发送，然后关闭客户端。
// 回调不得调用 Close，也不得在该生产者上阻塞等待。
func (p *Producer) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p.closeErr = p.client.Flush(ctx)
		p.client.Close()
	})
	return p.closeErr
}

// Send publishes one message synchronously | Send 同步发送一条消息。
func (p *Producer) Send(ctx context.Context, message *mq.Message) (mq.SendResponse, error) {
	return p.SendBatch(ctx, []*mq.Message{message})
}

// SendBatch validates all messages before sending. Kafka batches are not
// atomic; on success the response identifies the last input message.
// SendBatch 发送前校验全部消息。Kafka 批次不是原子的；
// 成功时响应只标识最后一条输入消息。
func (p *Producer) SendBatch(ctx context.Context, messages []*mq.Message) (mq.SendResponse, error) {
	if err := ctx.Err(); err != nil {
		return mq.SendResponse{}, err
	}
	if p.closed.Load() {
		return mq.SendResponse{}, ErrClosed
	}
	if len(messages) == 0 {
		return mq.SendResponse{}, errors.New("kafka: empty batch")
	}
	records := make([]*kgo.Record, len(messages))
	for i, message := range messages {
		record, err := toRecord(message)
		if err != nil {
			return mq.SendResponse{}, err
		}
		records[i] = record
	}
	results := p.client.ProduceSync(ctx, records...)
	if err := results.FirstErr(); err != nil {
		return mq.SendResponse{}, err
	}
	return response(records[len(records)-1], nil), nil
}

// SendAsync reports validation errors directly and delivery errors via
// callback. A nil return means the request was handed to franz-go, not that
// Kafka stored it.
// SendAsync 直接返回校验错误，投递错误经回调报告。返回 nil 只表示请求已交给
// franz-go，不代表 Kafka 已存储。
func (p *Producer) SendAsync(ctx context.Context, callback mq.AsyncSendCallback, message *mq.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed.Load() {
		return ErrClosed
	}
	if callback == nil {
		return errors.New("kafka: callback is nil")
	}
	record, err := toRecord(message)
	if err != nil {
		return err
	}
	p.client.TryProduce(ctx, record, func(record *kgo.Record, err error) { callback(ctx, response(record, err), err) })
	return nil
}
