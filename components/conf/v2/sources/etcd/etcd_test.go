package etcd_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"testing"

	"github.com/apus-run/gala/components/conf/v2/sources/etcd"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type reader struct {
	pairs  []*mvccpb.KeyValue
	key    string
	prefix bool
	calls  int
	err    error
}

func (r *reader) Get(_ context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	r.key, r.prefix = key, clientv3.IsOptsWithPrefix(opts)
	r.calls++
	return &clientv3.GetResponse{Kvs: r.pairs}, r.err
}

func pair(k, v string) *mvccpb.KeyValue { return &mvccpb.KeyValue{Key: []byte(k), Value: []byte(v)} }

func TestLoadModes(t *testing.T) {
	r := &reader{pairs: []*mvccpb.KeyValue{pair("/cfg", `{"N":1}`)}}
	doc := etcd.New(r, "/cfg", file.JSON)
	m, err := doc.Load(t.Context())
	if err != nil || m["N"] == nil || r.calls != 1 || r.prefix {
		t.Fatal(m, err)
	}
	if ch, err := doc.Watch(t.Context()); ch != nil || err == nil || err.Error() != "watch unsupported" || r.calls != 1 {
		t.Fatal(ch, err)
	}
	r.pairs[0].Value = []byte("SECRET")
	if _, err := doc.Load(t.Context()); err == nil || err.Error() != "decode failed at line 1, column 1" {
		t.Fatal(err)
	}
	for _, tc := range []struct{ prefix, key string }{{"app", "app/"}, {"app/", "app/"}, {"/app", "/app/"}, {"/", "/"}} {
		r.pairs = []*mvccpb.KeyValue{pair(tc.key+"child/port", "8"), pair(tc.key+"name", "")}
		before := r.calls
		m, err := etcd.NewPrefix(r, tc.prefix).Load(t.Context())
		if err != nil || !reflect.DeepEqual(m, map[string]any{"child": map[string]any{"port": "8"}, "name": ""}) || r.key != tc.key || !r.prefix || r.calls != before+1 {
			t.Fatal(tc, m, err)
		}
	}
	for _, pairs := range [][]*mvccpb.KeyValue{{pair("cfg/a", "1"), pair("cfg/a/b", "2")}, {pair("cfg/a//b", "1")}, {pair("cfgElse/a", "1")}} {
		r.pairs = pairs
		if _, err := etcd.NewPrefix(r, "cfg").Load(t.Context()); err == nil {
			t.Fatal("invalid prefix tree accepted")
		}
	}
	r.pairs = nil
	if _, err := doc.Load(t.Context()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := etcd.NewPrefix(r, "cfg").Load(t.Context()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	r.err = errors.New("SECRET")
	if _, err := doc.Load(t.Context()); err == nil || err.Error() != "read failed" {
		t.Fatal(err)
	}
}
