package kafka

import (
	"github.com/apus-run/gala/components/mq"
)

type Factory struct{}

func NewFactory() mq.Factory {
	return &Factory{}
}

// NewConsumer implements mq.Factory.
func (f *Factory) NewConsumer(mq.ConsumerConfig) (mq.Consumer, error) {
	panic("unimplemented")
}

// NewProducer implements mq.Factory.
func (f *Factory) NewProducer(mq.ProducerConfig) (mq.Producer, error) {
	panic("unimplemented")
}
