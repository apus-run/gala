package mq

import (
	"context"
)

// Consumer receives messages from a broker and dispatches them to a handler
// | Consumer 从 broker 接收消息并分发给处理器.
//
//go:generate mockgen -destination=mocks/consumer.go -package=mocks . Consumer
type Consumer interface {
	// Start begins consuming | Start 开始消费.
	Start() error
	// Close releases the consumer and its resources | Close 释放消费者及其资源.
	Close() error
	// RegisterHandler sets the handler invoked for each message | RegisterHandler 设置每条消息触发的处理器.
	RegisterHandler(ConsumerHandler)
}

// ConsumerHandler processes messages delivered by a Consumer | ConsumerHandler 处理 Consumer 投递的消息.
type ConsumerHandler interface {
	// HandleMessage processes one message; a non-nil error typically prevents
	// acknowledgement | HandleMessage 处理一条消息；返回非 nil 错误通常阻止确认.
	HandleMessage(context.Context, *MessageExt) error
}
