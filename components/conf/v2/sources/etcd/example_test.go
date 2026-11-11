package etcd_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/etcd"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// consume can be used by the application with the channel from src.Watch(ctx).
func consume(ctx context.Context, signals <-chan struct{}, reload func(context.Context) error, interval time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-signals:
			if !ok {
				return
			}
		}
		for ctx.Err() == nil && reload(ctx) != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}
}

// exampleClient supplies a confirmation and fails exactly one Get.
type exampleClient struct{ reads int }

func (c *exampleClient) Get(_ context.Context, key string, _ ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	c.reads++
	if c.reads == 2 {
		return nil, errors.New("temporarily unavailable")
	}
	value := `{"Port":8080}`
	if c.reads > 2 {
		value = `{"Port":9090}`
	}
	return &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{{Key: []byte(key), Value: []byte(value)}}}, nil
}
func (*exampleClient) Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
	ch := make(chan clientv3.WatchResponse, 1)
	ch <- clientv3.WatchResponse{Created: true}
	return ch
}

func ExampleSource_Watch() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := new(exampleClient)
	src := etcd.New(client, "app", file.JSON)
	cfg, err := conf.Open(ctx, struct{ Port int }{}, src)
	if err != nil {
		panic(err)
	}
	signals, err := src.Watch(ctx)
	if err != nil {
		panic(err)
	}
	consume(ctx, signals, func(ctx context.Context) error {
		if err := cfg.Reload(ctx); err != nil {
			return err
		}
		fmt.Println(cfg.Current().Port, client.reads)
		cancel()
		return nil
	}, time.Millisecond)
	// Output: 9090 3
}
