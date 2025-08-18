package registry

import (
	"context"

	"github.com/pkg/errors"

	"github.com/apus-run/gala/components/mq"

	json "github.com/apus-run/gala/pkg/jsonx"
	"github.com/apus-run/gala/pkg/lang/ptr"
	"github.com/apus-run/gala/pkg/lang/safego"
)

type defaultConsumerRegistry struct {
	factory mq.Factory
	workers []mq.ConsumerWorker
}

func NewConsumerRegistry(factory mq.Factory) mq.ConsumerRegistry {
	return &defaultConsumerRegistry{factory: factory}
}

func (d *defaultConsumerRegistry) Register(worker []mq.ConsumerWorker) mq.ConsumerRegistry {
	d.workers = append(d.workers, worker...)
	return d
}

func (d *defaultConsumerRegistry) StartAll(ctx context.Context) error {
	for _, worker := range d.workers {
		cfg, err := worker.ConsumerCfg(ctx)
		if err != nil {
			return err
		}

		consumer, err := d.factory.NewConsumer(ptr.From(cfg))
		if err != nil {
			// 如果创建消费者失败，返回错误
			return errors.Wrapf(err, "NewConsumer fail, cfg: %v", json.Jsonify(cfg))
		}

		consumer.RegisterHandler(newSafeConsumerWrapper(worker))
		if err := consumer.Start(); err != nil {
			// 关闭已启动的消费者
			return errors.Wrapf(err, "StartConsumer fail, cfg: %v", json.Jsonify(cfg))
		}
	}
	return nil
}

type safeConsumerHandlerDecorator struct {
	handler mq.ConsumerHandler
}

func (s *safeConsumerHandlerDecorator) HandleMessage(ctx context.Context, msg *mq.MessageExt) error {
	defer safego.Recovery(ctx)
	return s.handler.HandleMessage(ctx, msg)
}

func newSafeConsumerWrapper(h mq.ConsumerHandler) mq.ConsumerHandler {
	return &safeConsumerHandlerDecorator{handler: h}
}
