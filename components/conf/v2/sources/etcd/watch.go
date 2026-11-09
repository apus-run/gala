package etcd

import (
	"context"
	"errors"
	"reflect"

	conf "github.com/apus-run/gala/components/conf/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Watch starts at the revision following a current read, so the watch setup
// cannot miss a subsequent update. It never closes the borrowed etcd client.
func (s *Source) Watch(ctx context.Context) (conf.Watcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.client == nil || (reflect.ValueOf(s.client).Kind() == reflect.Pointer && reflect.ValueOf(s.client).IsNil()) || s.key == "" {
		return nil, conf.SourceError(conf.StageConfig, "", "", nil)
	}

	client, ok := s.client.(interface {
		Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
	})
	if !ok {
		return nil, conf.ErrWatchUnsupported
	}
	response, err := s.client.Get(ctx, s.key)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Header == nil {
		return nil, errors.New("missing etcd revision")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	ch := client.Watch(watchCtx, s.key, clientv3.WithRev(response.Header.Revision+1))
	return &etcdWatcher{ch: ch, cancel: cancel}, nil
}

type etcdWatcher struct {
	ch     clientv3.WatchChan
	cancel context.CancelFunc
}

func (w *etcdWatcher) Next(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case response, ok := <-w.ch:
			if !ok {
				return errors.New("etcd watch closed")
			}
			if err := response.Err(); err != nil {
				return err
			}
			if len(response.Events) > 0 {
				return nil
			}
		}
	}
}
func (w *etcdWatcher) Close() error { w.cancel(); return nil }
