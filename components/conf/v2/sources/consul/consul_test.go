package consul_test

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/consul"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"github.com/hashicorp/consul/api"
)

type getter func(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)

func (f getter) Get(k string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
	return f(k, q)
}

type lister func(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)

func (f lister) List(k string, q *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
	return f(k, q)
}

func TestLoadModes(t *testing.T) {
	raw := []byte(`{"Port":7}`)
	calls := 0
	src := consul.New(getter(func(k string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		calls++
		if k != "cfg" || !q.RequireConsistent {
			t.Fatal(k, q)
		}
		return &api.KVPair{Key: k, Value: raw}, &api.QueryMeta{}, nil
	}), "cfg", file.JSON)
	m, err := src.Load(t.Context())
	if err != nil || m["Port"] == nil || calls != 1 {
		t.Fatal(m, err)
	}
	raw = []byte("SECRET")
	if _, err := src.Load(t.Context()); err == nil || err.Error() != "decode failed" {
		t.Fatal(err)
	}
	var pairs api.KVPairs
	var filtered bool
	list := lister(func(k string, q *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
		calls++
		if k != "app/" || !q.RequireConsistent {
			t.Fatal(k, q)
		}
		return pairs, &api.QueryMeta{ResultsFilteredByACLs: filtered}, nil
	})
	for _, p := range []string{"app", "app/", "/app"} {
		pairs = api.KVPairs{{Key: "app/sub/", Value: nil}, {Key: "app/sub/N", Value: []byte("8")}, {Key: "app/name"}}
		before := calls
		m, err := consul.NewPrefix(list, p).Load(t.Context())
		if err != nil || !reflect.DeepEqual(m, map[string]any{"sub": map[string]any{"N": "8"}, "name": ""}) || calls != before+1 {
			t.Fatal(m, err)
		}
	}
	before := calls
	for _, p := range []string{"", "/", "//app", "app//leaf"} {
		s := consul.NewPrefix(list, p)
		if _, err := s.Load(t.Context()); err == nil {
			t.Fatal(p)
		}
		if ch, err := s.Watch(t.Context()); ch != nil || err == nil {
			t.Fatal(p)
		}
	}
	if calls != before {
		t.Fatal("queried invalid prefix")
	}
	src = consul.NewPrefix(list, "app")
	optional := conf.SourceFunc(func(ctx context.Context) (map[string]any, error) {
		m, err := src.Load(ctx)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return m, err
	})
	pairs = nil
	c, err := conf.Open(t.Context(), struct{ Port int }{Port: 8}, optional)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	filtered = true
	for _, visible := range []api.KVPairs{nil, {{Key: "app/Port", Value: []byte("9")}}} {
		pairs = visible
		err := c.Reload(t.Context())
		var ce *conf.Error
		if !errors.As(err, &ce) || ce.Stage != conf.StageLoad || !errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) || c.Current() != old {
			t.Fatal(err)
		}
	}
}

func TestSDKMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !q.Has("consistent") || q.Has("stale") || q.Has("cached") {
			t.Error(q)
		}
		w.Header().Set("X-Consul-Index", "8")
		if r.Header.Get("X-Test-Contact") == "1" {
			w.Header().Set("X-Consul-LastContact", "0")
		}
		if r.Header.Get("X-Test-Filtered") == "1" {
			w.Header().Set("X-Consul-Results-Filtered-By-ACLs", "true")
		}
		_, _ = w.Write([]byte(`[{"Key":"app/Port","Value":"OQ=="}]`))
	}))
	defer srv.Close()
	for _, tc := range []struct{ filtered, contact bool }{{true, true}, {false, true}, {true, false}} {
		filtered, contact := tc.filtered, tc.contact
		cfg := api.DefaultConfig()
		cfg.Address = srv.URL
		client, err := api.NewClient(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if filtered {
			client.AddHeader("X-Test-Filtered", "1")
		}
		if contact {
			client.AddHeader("X-Test-Contact", "1")
		}
		m, err := consul.NewPrefix(client.KV(), "app").Load(t.Context())
		if filtered && contact {
			if !errors.Is(err, fs.ErrPermission) {
				t.Fatal(err)
			}
		} else if err != nil || m["Port"] != "9" {
			t.Fatal(m, err)
		}
	}
}

func TestInvalidClientsFailBeforeIO(t *testing.T) {
	var get getter
	var list lister
	unused := getter(func(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
		t.Fatal("queried without a decoder")
		return nil, nil, nil
	})
	for _, source := range []*consul.Source{
		consul.New(get, "app", file.JSON),
		consul.NewPrefix(list, "app"),
		consul.New(unused, "app", nil),
	} {
		if _, err := source.Load(t.Context()); err == nil {
			t.Fatal("invalid source accepted by Load")
		}
		if ch, err := source.Watch(t.Context()); ch != nil || err == nil {
			t.Fatal("invalid source accepted by Watch")
		}
	}
}
