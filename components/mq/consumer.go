package mq

import (
	"context"
)

//go:generate mockgen -destination=mocks/consumer.go -package=mocks . Consumer
type Consumer interface {
	Start() error
	Close() error
	RegisterHandler(ConsumerHandler)
}

type ConsumerHandler interface {
	HandleMessage(context.Context, *MessageExt) error
}
