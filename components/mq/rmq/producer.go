package rmq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"

	"github.com/apus-run/gala/components/mq"
)

type Producer struct {
	producer rocketmq.Producer
}

func (p *Producer) Start() error {
	return p.producer.Start()
}

func (p *Producer) Close() error {
	return p.producer.Shutdown()
}

func (p *Producer) Send(ctx context.Context, message *mq.Message) (mq.SendResponse, error) {
	if err := ctx.Err(); err != nil {
		return mq.SendResponse{}, err
	}
	if err := validateMessage(message); err != nil {
		return mq.SendResponse{}, err
	}
	return sendResponse(p.producer.SendSync(ctx, p.convertMessage(message)))
}

func (p *Producer) SendBatch(ctx context.Context, messages []*mq.Message) (mq.SendResponse, error) {
	if err := ctx.Err(); err != nil {
		return mq.SendResponse{}, err
	}
	if len(messages) == 0 {
		return mq.SendResponse{}, errors.New("rmq: empty batch")
	}
	msgs := make([]*primitive.Message, 0, len(messages))
	for i, message := range messages {
		if err := validateMessage(message); err != nil {
			return mq.SendResponse{}, fmt.Errorf("rmq: message %d: %w", i, err)
		}
		if message.Topic != messages[0].Topic {
			return mq.SendResponse{}, errors.New("rmq: batch messages must share a topic")
		}
		// The SDK drops the outer batch's delay and sharding properties.
		if len(messages) > 1 && (message.DeferDuration > 0 || message.PartitionKey != "" ||
			message.Properties[primitive.PropertyDelayTimeLevel] != "" ||
			message.Properties[primitive.PropertyShardingKey] != "") {
			return mq.SendResponse{}, fmt.Errorf("rmq: delayed or orderly batch: %w", mq.ErrUnsupported)
		}
		msgs = append(msgs, p.convertMessage(message))
	}
	return sendResponse(p.producer.SendSync(ctx, msgs...))
}

func (p *Producer) SendAsync(ctx context.Context, callback mq.AsyncSendCallback, message *mq.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if callback == nil {
		return errors.New("rmq: callback is nil")
	}
	if err := validateMessage(message); err != nil {
		return err
	}
	return p.producer.SendAsync(ctx, func(ctx context.Context, result *primitive.SendResult, err error) {
		resp, err := sendResponse(result, err)
		callback(ctx, resp, err)
	}, p.convertMessage(message))
}

func validateMessage(message *mq.Message) error {
	if message == nil {
		return errors.New("rmq: message is nil")
	}
	if message.Topic == "" {
		return errors.New("rmq: topic is empty")
	}
	if message.DeferDuration < 0 {
		return errors.New("rmq: negative delay")
	}
	return nil
}

// A non-OK result can still have a message ID; preserve it for reconciliation.
func sendResponse(result *primitive.SendResult, err error) (mq.SendResponse, error) {
	var resp mq.SendResponse
	if result != nil {
		resp.MessageID = result.MsgID
		resp.Offset = result.QueueOffset
	}
	if err != nil {
		return resp, err
	}
	if result == nil {
		return resp, errors.New("rmq: missing send result")
	}
	if result.Status != primitive.SendOK {
		return resp, fmt.Errorf("rmq: send status %d", result.Status)
	}
	return resp, nil
}

func (p *Producer) convertMessage(message *mq.Message) *primitive.Message {
	msg := primitive.NewMessage(message.Topic, bytes.Clone(message.Body))
	if len(message.Properties) > 0 {
		msg.WithProperties(maps.Clone(message.Properties))
	}
	// Explicit fields take precedence over conflicting transport properties.
	if message.Tag != "" {
		msg.WithTag(message.Tag)
	}
	if message.PartitionKey != "" {
		msg.WithShardingKey(message.PartitionKey)
	}
	if message.DeferDuration > 0 {
		msg.WithDelayTimeLevel(p.toDelayLevel(message.DeferDuration))
	}
	return msg
}

// rocketmq 默认delayLevel对应的秒数
var delayLevels = [...]int{
	1,    // level 1: 1s
	5,    // level 2: 5s
	10,   // level 3: 10s
	30,   // level 4: 30s
	60,   // level 5: 1m
	120,  // level 6: 2m
	180,  // level 7: 3m
	240,  // level 8: 4m
	300,  // level 9: 5m
	360,  // level 10: 6m
	420,  // level 11: 7m
	480,  // level 12: 8m
	540,  // level 13: 9m
	600,  // level 14: 10m
	1200, // level 15: 20m
	1800, // level 16: 30m
	3600, // level 17: 1h
	7200, // level 18: 2h
}

func (p *Producer) toDelayLevel(d time.Duration) int {
	// 找到第一个大于等于目标值的级别
	for i, level := range delayLevels {
		if d <= time.Duration(level)*time.Second {
			return i + 1 // 级别从1开始
		}
	}

	// 超过最大级别返回最大值
	return len(delayLevels)
}
