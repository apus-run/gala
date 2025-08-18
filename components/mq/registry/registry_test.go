package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/apus-run/gala/components/mq"
	"github.com/apus-run/gala/components/mq/mocks"
)

func TestDefaultConsumerRegistry_StartAll(t *testing.T) {
	tests := []struct {
		name          string
		workers       []mq.ConsumerWorker
		setupMocks    func(*mocks.MockFactory, []*mocks.MockConsumer, []*mocks.MockConsumerWorker)
		expectedError error
	}{
		{
			name: "successfully start all workers",
			workers: []mq.ConsumerWorker{
				mocks.NewMockConsumerWorker(gomock.NewController(t)),
				mocks.NewMockConsumerWorker(gomock.NewController(t)),
			},
			setupMocks: func(factory *mocks.MockFactory, consumers []*mocks.MockConsumer, workers []*mocks.MockConsumerWorker) {
				cfg := &mq.ConsumerConfig{}
				for i := range workers {
					workers[i].EXPECT().ConsumerCfg(gomock.Any()).Return(cfg, nil)
					consumers[i].EXPECT().RegisterHandler(gomock.Any()).Return()
					consumers[i].EXPECT().Start().Return(nil)
					factory.EXPECT().NewConsumer(gomock.Any()).Return(consumers[i], nil)
				}
			},
			expectedError: nil,
		},
		{
			name: "fail to get consumer config",
			workers: []mq.ConsumerWorker{
				mocks.NewMockConsumerWorker(gomock.NewController(t)),
			},
			setupMocks: func(factory *mocks.MockFactory, consumers []*mocks.MockConsumer, workers []*mocks.MockConsumerWorker) {
				workers[0].EXPECT().ConsumerCfg(gomock.Any()).Return(nil, errors.New("config error"))
			},
			expectedError: errors.New("config error"),
		},
		{
			name: "fail to create consumer",
			workers: []mq.ConsumerWorker{
				mocks.NewMockConsumerWorker(gomock.NewController(t)),
			},
			setupMocks: func(factory *mocks.MockFactory, consumers []*mocks.MockConsumer, workers []*mocks.MockConsumerWorker) {
				cfg := &mq.ConsumerConfig{}
				workers[0].EXPECT().ConsumerCfg(gomock.Any()).Return(cfg, nil)
				factory.EXPECT().NewConsumer(gomock.Any()).Return(nil, errors.New("create error"))
			},
			expectedError: errors.New("create error"),
		},
		{
			name: "fail to start consumer",
			workers: []mq.ConsumerWorker{
				mocks.NewMockConsumerWorker(gomock.NewController(t)),
			},
			setupMocks: func(factory *mocks.MockFactory, consumers []*mocks.MockConsumer, workers []*mocks.MockConsumerWorker) {
				cfg := &mq.ConsumerConfig{}
				workers[0].EXPECT().ConsumerCfg(gomock.Any()).Return(cfg, nil)
				consumers[0].EXPECT().RegisterHandler(gomock.Any()).Return()
				consumers[0].EXPECT().Start().Return(errors.New("start error"))
				factory.EXPECT().NewConsumer(gomock.Any()).Return(consumers[0], nil)
			},
			expectedError: errors.New("start error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			factory := mocks.NewMockFactory(ctrl)
			consumers := make([]*mocks.MockConsumer, len(tt.workers))
			workers := make([]*mocks.MockConsumerWorker, len(tt.workers))

			for i := range tt.workers {
				consumers[i] = mocks.NewMockConsumer(ctrl)
				workers[i] = tt.workers[i].(*mocks.MockConsumerWorker)
			}

			tt.setupMocks(factory, consumers, workers)

			registry := NewConsumerRegistry(factory).Register(tt.workers)

			err := registry.StartAll(context.Background())
			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSafeConsumerHandlerDecorator_HandleMessage(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(*mocks.MockConsumerWorker)
		expectedError error
	}{
		{
			name: "successfully handle message",
			setupMock: func(w *mocks.MockConsumerWorker) {
				w.EXPECT().HandleMessage(gomock.Any(), gomock.Any()).Return(nil)
			},
			expectedError: nil,
		},
		{
			name: "handler returns error",
			setupMock: func(w *mocks.MockConsumerWorker) {
				w.EXPECT().HandleMessage(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, *mq.MessageExt) error {
					panic("test panic")
				})
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			handler := mocks.NewMockConsumerWorker(ctrl)
			tt.setupMock(handler)

			decorator := &safeConsumerHandlerDecorator{handler: handler}
			err := decorator.HandleMessage(context.Background(), &mq.MessageExt{})

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
