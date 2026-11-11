package etcd_test

import (
	"context"
	"testing"
	"testing/synctest"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/etcd"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/metadata"
)

type watching struct {
	reader
	streams chan chan clientv3.WatchResponse
	targets chan string
	t       *testing.T
}

func (w *watching) Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	md, _ := metadata.FromOutgoingContext(ctx)
	if len(md.Get(rpctypes.MetadataRequireLeaderKey)) == 0 {
		w.t.Error("leader not required")
	}
	if !clientv3.IsOptsWithPrefix(opts) {
		w.t.Error("watch missed prefix option")
	}
	w.targets <- key
	stream := make(chan clientv3.WatchResponse, 4)
	w.streams <- stream
	return stream
}

func TestWatchReconcilesAndRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &watching{streams: make(chan chan clientv3.WatchResponse, 1), targets: make(chan string, 1), t: t}
		src := etcd.NewPrefix(w, "cfg")
		w.pairs = []*mvccpb.KeyValue{pair("cfg/Port", "1")}
		c, err := conf.Open(t.Context(), struct{ Port int }{}, src)
		if err != nil {
			t.Fatal(err)
		}
		w.pairs = []*mvccpb.KeyValue{pair("cfg/Port", "2")}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ch, err := src.Watch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		stream := <-w.streams
		if <-w.targets != "cfg/" {
			t.Fatal("wrong watch target")
		}
		stream <- clientv3.WatchResponse{Created: true}
		<-ch
		if err := c.Reload(ctx); err != nil || c.Current().Port != 2 {
			t.Fatal(c.Current(), err)
		}
		for range 3 {
			stream <- clientv3.WatchResponse{Events: []*clientv3.Event{{}}}
		}
		synctest.Wait()
		if len(ch) != 1 {
			t.Fatalf("signals=%d", len(ch))
		}
		<-ch
		stream <- clientv3.WatchResponse{Canceled: true, CompactRevision: 7}
		w.pairs = []*mvccpb.KeyValue{pair("cfg/Port", "3")}
		stream = <-w.streams
		<-w.targets
		stream <- clientv3.WatchResponse{Created: true}
		<-ch
		if err := c.Reload(ctx); err != nil || c.Current().Port != 3 {
			t.Fatal(c.Current(), err)
		}
		close(stream)
		synctest.Wait()
		cancel()
		if _, ok := <-ch; ok {
			t.Fatal("signal after cancellation")
		}
	})
}
