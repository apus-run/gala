package mq

import (
	"context"
)

// Producer publishes messages to a broker | Producer 向 broker 发布消息.
//
//go:generate mockgen -destination=mocks/producer.go -package=mocks . Producer
type Producer interface {
	// Start prepares the producer for sending | Start 让生产者就绪.
	Start() error
	// Close releases the producer and its resources | Close 释放生产者及其资源.
	Close() error
	// Send publishes one message synchronously | Send 同步发送一条消息.
	Send(ctx context.Context, message *Message) (SendResponse, error)
	// SendBatch publishes multiple messages in one call | SendBatch 一次调用发送多条消息.
	SendBatch(ctx context.Context, messages []*Message) (SendResponse, error)
	// SendAsync publishes one message and reports the outcome via callback
	// | SendAsync 异步发送一条消息，结果经回调报告.
	SendAsync(ctx context.Context, callback AsyncSendCallback, message *Message) error
}

// AsyncSendCallback receives the outcome of an async send | AsyncSendCallback 接收异步发送的结果.
type AsyncSendCallback func(ctx context.Context, sendResponse SendResponse, err error)

// SendResponse reports the broker-assigned id and offset of a send | SendResponse 报告发送后 broker 分配的 id 与偏移量.
type SendResponse struct {
	// MessageID is the broker-assigned message id | broker 分配的消息 id
	MessageID string
	// Offset is the write offset in the log, when the backend exposes one | 写入日志的偏移量，以后端是否暴露为准
	Offset int64
}
