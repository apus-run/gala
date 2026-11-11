package etcd

import (
	"context"
	"errors"

	"github.com/apus-run/gala/components/conf/v2/internal/backoff"
	"github.com/apus-run/gala/components/conf/v2/internal/remote"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Watch signals after initial confirmation, every new subscription and data
// changes. The channel closes on cancellation. A read-only client may Load
// without supporting Watch.
func (s *Source) Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.valid() {
		return nil, errors.New("invalid etcd source")
	}
	client, ok := s.client.(interface {
		Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
	})
	if !ok {
		return nil, errors.New("watch unsupported")
	}
	opts := []clientv3.OpOption{clientv3.WithCreatedNotify()}
	if s.prefix {
		opts = append(opts, clientv3.WithPrefix())
	}
	signals := make(chan struct{}, 1)
	go func() {
		defer close(signals)
		var retry backoff.Delay
		for ctx.Err() == nil {
			streamCtx, cancel := context.WithCancel(clientv3.WithRequireLeader(ctx))
			stream := client.Watch(streamCtx, s.key, opts...)
			if consume(ctx, stream, signals) {
				retry = 0
			}
			cancel()
			if !retry.Wait(ctx) {
				return
			}
		}
	}()
	return signals, nil
}

func consume(ctx context.Context, stream clientv3.WatchChan, signals chan<- struct{}) (confirmed bool) {
	if stream == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case response, ok := <-stream:
			if !ok || response.Canceled || response.Err() != nil {
				return
			}
			if response.Created || len(response.Events) > 0 {
				confirmed = true
				remote.Signal(signals)
			}
		}
	}
}
