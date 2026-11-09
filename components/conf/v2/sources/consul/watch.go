package consul

import (
	"context"
	"errors"
	"reflect"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/hashicorp/consul/api"
)

// Watch uses Consul blocking queries. Close never closes the borrowed client.
func (s *Source) Watch(ctx context.Context) (conf.Watcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.client == nil || (reflect.ValueOf(s.client).Kind() == reflect.Pointer && reflect.ValueOf(s.client).IsNil()) || s.key == "" {
		return nil, conf.SourceError(conf.StageConfig, "", "", nil)
	}

	_, meta, err := s.client.Get(s.key, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, errors.New("missing Consul query index")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	return &consulWatcher{source: s, index: max(meta.LastIndex, 1), ctx: watchCtx, cancel: cancel}, nil
}

type consulWatcher struct {
	source *Source
	index  uint64
	ctx    context.Context
	cancel context.CancelFunc
}

func (w *consulWatcher) Next(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.ctx, cancel)
	defer stop()
	defer cancel()
	for {
		_, meta, err := w.source.client.Get(w.source.key, (&api.QueryOptions{WaitIndex: w.index, WaitTime: time.Minute}).WithContext(ctx))
		if err != nil {
			return err
		}
		if meta == nil {
			return errors.New("missing Consul query index")
		}
		index := max(meta.LastIndex, 1)
		if index != w.index {
			w.index = index
			return nil
		}
	}
}
func (w *consulWatcher) Close() error { w.cancel(); return nil }
