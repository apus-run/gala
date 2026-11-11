package consul_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/consul"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"github.com/hashicorp/consul/api"
)

func TestBlockingIndexRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type reply struct {
			index    uint64
			filtered bool
			err      error
		}
		replies := make(chan reply)
		queries := make(chan uint64)
		src := consul.NewPrefix(lister(func(key string, q *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
			if key != "app/" || !q.RequireConsistent {
				t.Error(key, q)
			}
			select {
			case queries <- q.WaitIndex:
			case <-q.Context().Done():
				return nil, nil, q.Context().Err()
			}
			select {
			case r := <-replies:
				return nil, &api.QueryMeta{LastIndex: r.index, ResultsFilteredByACLs: r.filtered}, r.err
			case <-q.Context().Done():
				return nil, nil, q.Context().Err()
			}
		}), "/app/")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ch, err := src.Watch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			wait   uint64
			r      reply
			signal bool
		}{
			{0, reply{}, true}, {1, reply{}, true}, {1, reply{index: 1}, true}, {1, reply{index: 1}, false}, {1, reply{index: 7}, true}, {7, reply{index: 3}, false},
			{0, reply{index: 3}, true}, {3, reply{filtered: true}, false}, {0, reply{err: errors.New("SECRET")}, false}, {0, reply{index: 8}, true},
		} {
			if got := <-queries; got != tc.wait {
				t.Fatalf("WaitIndex=%d want %d", got, tc.wait)
			}
			replies <- tc.r
			synctest.Wait()
			select {
			case <-ch:
				if !tc.signal {
					t.Fatal("unexpected signal", tc)
				}
			default:
				if tc.signal {
					t.Fatal("missing signal", tc)
				}
			}
		}
		cancel()
		if _, ok := <-ch; ok {
			t.Fatal("channel still open")
		}
	})
}

func TestSingleKeyWatchReconcilesBeforeFirstConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		value := "1"
		queries := 0

		source := consul.New(getter(func(key string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
			if key != "cfg" || !q.RequireConsistent {
				t.Error("inconsistent target", key, q)
			}
			if q.WaitIndex != 0 {
				<-q.Context().Done()
				return nil, nil, q.Context().Err()
			}
			queries++

			if queries == 3 {
				return nil, nil, errors.New("read unavailable")
			}
			return &api.KVPair{Key: key, Value: []byte(`{"Port":` + value + `}`)}, &api.QueryMeta{LastIndex: 4}, nil
		}), "cfg", file.JSON)
		c, err := conf.Open(ctx, struct{ Port int }{}, source)
		if err != nil {
			t.Fatal(err)
		}
		old := c.Current()
		value = "2" // the only write is before the watch baseline
		signals, err := source.Watch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		<-signals
		if err := c.Reload(ctx); err == nil || c.Current() != old {
			t.Fatalf("failed read changed snapshot: %v", err)
		}
		// Retry the same invalidation without waiting for a second write.
		if err := c.Reload(ctx); err != nil || c.Current().Port != 2 || queries != 4 {
			t.Fatalf("retry: %v, calls %d", err, queries)
		}
		cancel()
		if _, ok := <-signals; ok {
			t.Fatal("unexpected second invalidation")
		}
	})
}
