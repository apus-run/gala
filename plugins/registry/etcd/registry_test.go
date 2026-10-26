package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/registry"
)

// Exercise the real client/session lifecycle through etcd's service interfaces.
// No running cluster or network connection is needed for these regression tests.
type testKV struct {
	clientv3.KV
	get func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
	put func(context.Context, string, string, ...clientv3.OpOption) (*clientv3.PutResponse, error)
	del func(context.Context, string, ...clientv3.OpOption) (*clientv3.DeleteResponse, error)
}

func (s *testKV) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return s.get(ctx, key, opts...)
}
func (s *testKV) Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	return s.put(ctx, key, val, opts...)
}
func (s *testKV) Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	return s.del(ctx, key, opts...)
}

type testWatcher struct {
	clientv3.Watcher
	watch func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
}

func (s *testWatcher) Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	return s.watch(ctx, key, opts...)
}
func (*testWatcher) Close() error { return nil }

type testLease struct {
	clientv3.Lease
	expire  chan struct{}
	grants  atomic.Int32
	revokes atomic.Int32
	revoke  func(context.Context, clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error)
}

func (s *testLease) Grant(context.Context, int64) (*clientv3.LeaseGrantResponse, error) {
	s.grants.Add(1)
	return &clientv3.LeaseGrantResponse{ID: 123}, nil
}
func (s *testLease) KeepAlive(ctx context.Context, _ clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	ch := make(chan *clientv3.LeaseKeepAliveResponse)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
		case <-s.expire:
		}
	}()
	return ch, nil
}
func (s *testLease) Revoke(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
	s.revokes.Add(1)
	if s.revoke != nil {
		return s.revoke(ctx, id)
	}
	return &clientv3.LeaseRevokeResponse{}, nil
}
func (*testLease) Close() error { return nil }

type fixture struct {
	r         *Registry
	d         *Discovery
	kv        *testKV
	watcher   *testWatcher
	lease     *testLease
	responses chan clientv3.WatchResponse
	watchCtx  context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{responses: make(chan clientv3.WatchResponse, 8)}
	f.responses <- clientv3.WatchResponse{Created: true}
	f.kv = &testKV{
		get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return &clientv3.GetResponse{Header: &pb.ResponseHeader{Revision: 10}}, nil
		},
		put: func(context.Context, string, string, ...clientv3.OpOption) (*clientv3.PutResponse, error) {
			return &clientv3.PutResponse{}, nil
		},
		del: func(context.Context, string, ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
			return &clientv3.DeleteResponse{}, nil
		},
	}
	f.watcher = &testWatcher{watch: func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
		f.watchCtx = ctx
		return f.responses
	}}
	f.lease = &testLease{expire: make(chan struct{})}
	client := clientv3.NewCtxClient(t.Context())
	client.KV, client.Watcher, client.Lease = f.kv, f.watcher, f.lease
	var err error
	f.r, err = NewRegistry(client)
	if err != nil {
		t.Fatal(err)
	}
	f.d, err = NewDiscovery(client)
	if err != nil {
		_ = f.r.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.Close(); _ = f.r.Close(); _ = client.Close() })
	return f
}
func instanceKV(t *testing.T, id string) *mvccpb.KeyValue {
	t.Helper()
	value, err := json.Marshal(&registry.ServiceInstance{Name: "svc", ID: id, Metadata: map[string]string{"zone": "a"}, Endpoints: []string{"grpc://localhost:9000"}})
	if err != nil {
		t.Fatal(err)
	}
	return &mvccpb.KeyValue{Key: []byte(defaultPrefix + "/svc/" + id), Value: value}
}
func receive(t *testing.T, ch <-chan registry.Event) registry.Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("subscription closed unexpectedly")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return registry.Event{}
	}
}
func drained(t *testing.T, ch <-chan registry.Event) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("subscription did not close")
		}
	}
}

func TestRegisterGetAndDeregister(t *testing.T) {
	f := newFixture(t)
	var saved *mvccpb.KeyValue
	f.kv.put = func(_ context.Context, key, value string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
		if !reflect.DeepEqual(clientv3.OpPut(key, value, opts...), clientv3.OpPut(key, value, clientv3.WithLease(123))) {
			t.Fatal("registration must be attached to the session lease")
		}
		saved = &mvccpb.KeyValue{Key: []byte(key), Value: []byte(value)}
		return &clientv3.PutResponse{}, nil
	}
	ins := &registry.ServiceInstance{Name: "svc", Metadata: map[string]string{"zone": "a"}}
	if err := f.r.Register(t.Context(), ins); err != nil {
		t.Fatal(err)
	}
	if ins.ID == "" || string(saved.Key) != defaultPrefix+"/svc/"+ins.ID {
		t.Fatalf("missing generated ID: %+v", ins)
	}
	f.kv.get = func(_ context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		if key != defaultPrefix+"/svc/" || !clientv3.OpGet(key, opts...).IsOptsWithPrefix() {
			t.Fatalf("unsafe prefix: %q", key)
		}
		return &clientv3.GetResponse{Header: &pb.ResponseHeader{Revision: 10}, Kvs: []*mvccpb.KeyValue{saved}}, nil
	}
	got, err := f.d.GetService(t.Context(), "svc")
	if err != nil || len(got) != 1 || !ins.Equal(got[0]) {
		t.Fatalf("GetService: %v, %v", got, err)
	}
	got[0].Metadata["zone"] = "changed"
	again, err := f.d.GetService(t.Context(), "svc")
	if err != nil || again[0].Metadata["zone"] != "a" {
		t.Fatal("returned instances share mutable state")
	}
	f.kv.del = func(_ context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
		if key != string(saved.Key) || clientv3.OpDelete(key, opts...).IsOptsWithPrefix() {
			t.Fatal("Deregister must delete exactly one instance")
		}
		return &clientv3.DeleteResponse{}, nil
	}
	if err := f.r.Deregister(t.Context(), ins); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRetryKeepsGeneratedID(t *testing.T) {
	f := newFixture(t)
	var keys []string
	f.kv.put = func(_ context.Context, key, _ string, _ ...clientv3.OpOption) (*clientv3.PutResponse, error) {
		keys = append(keys, key)
		return nil, context.DeadlineExceeded
	}
	ins := &registry.ServiceInstance{Name: "svc"}
	for range 2 {
		if err := f.r.Register(t.Context(), ins); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if ins.ID == "" || keys[0] != keys[1] {
		t.Fatalf("retry changed registration key: %v", keys)
	}
}

func TestInvalidArguments(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		ins  *registry.ServiceInstance
		want error
	}{
		{nil, registry.ErrNilInstance},
		{&registry.ServiceInstance{}, registry.ErrInvalidName},
		{&registry.ServiceInstance{Name: "a/b"}, registry.ErrInvalidName},
		{&registry.ServiceInstance{Name: "svc", ID: "a/b"}, registry.ErrInvalidID},
	} {
		if err := f.r.Register(t.Context(), tc.ins); !errors.Is(err, tc.want) {
			t.Errorf("Register: %v, want %v", err, tc.want)
		}
		if err := f.r.Deregister(t.Context(), tc.ins); !errors.Is(err, tc.want) {
			t.Errorf("Deregister: %v, want %v", err, tc.want)
		}
	}
	if err := f.r.Deregister(t.Context(), &registry.ServiceInstance{Name: "svc"}); !errors.Is(err, registry.ErrInvalidID) {
		t.Fatal(err)
	}
	if _, err := f.d.GetService(t.Context(), "a/b"); !errors.Is(err, registry.ErrInvalidName) {
		t.Fatal(err)
	}
	if _, err := f.d.Subscribe(t.Context(), ""); !errors.Is(err, registry.ErrInvalidName) {
		t.Fatal(err)
	}
	if _, err := NewRegistry(nil); err == nil {
		t.Fatal("nil client accepted")
	}
}

func TestSubscribeReplayAndStream(t *testing.T) {
	f := newFixture(t)
	f.kv.get = func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Header: &pb.ResponseHeader{Revision: 42}, Kvs: []*mvccpb.KeyValue{instanceKV(t, "a")}}, nil
	}
	f.watcher.watch = func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
		f.watchCtx = ctx
		op := clientv3.OpGet(key, opts...)
		if op.Rev() != 43 || key != defaultPrefix+"/svc/" || !op.IsOptsWithPrefix() {
			t.Fatalf("watch must resume after snapshot: key=%s rev=%d", key, op.Rev())
		}
		// This update occurs after the snapshot and before Watch returns.
		f.responses <- clientv3.WatchResponse{Events: []*clientv3.Event{{Type: mvccpb.PUT, Kv: instanceKV(t, "b")}}}
		return f.responses
	}
	ch, err := f.d.Subscribe(t.Context(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		ev := receive(t, ch)
		if ev.Type != registry.EventTypeAdd || ev.Instance.ID != id || len(ev.Instance.Endpoints) != 1 || ev.Instance.Metadata["zone"] != "a" {
			t.Fatalf("bad replay/update: %+v", ev)
		}
	}
	for _, prev := range []*mvccpb.KeyValue{instanceKV(t, "b"), nil} {
		f.responses <- clientv3.WatchResponse{Events: []*clientv3.Event{{Type: mvccpb.DELETE, Kv: &mvccpb.KeyValue{Key: []byte(defaultPrefix + "/svc/b")}, PrevKv: prev}}}
		ev := receive(t, ch)
		if ev.Type != registry.EventTypeDelete || ev.Instance.Name != "svc" || ev.Instance.ID != "b" {
			t.Fatalf("bad deletion: %+v", ev)
		}
	}
}

func TestSubscribeCreationFailures(t *testing.T) {
	for _, mode := range []string{"read error", "compacted", "watch closed", "caller canceled"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "read error":
				f.kv.get = func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
					return nil, errors.New("permission denied")
				}
			case "compacted":
				<-f.responses
				f.responses <- clientv3.WatchResponse{CompactRevision: 11, Canceled: true}
			case "watch closed":
				<-f.responses
				close(f.responses)
			case "caller canceled":
				cancel()
			}
			ch, err := f.d.Subscribe(ctx, "svc")
			if err == nil || ch != nil {
				t.Fatalf("expected creation error, got %v, %v", ch, err)
			}
		})
	}
}

func TestSubscriptionTermination(t *testing.T) {
	for _, mode := range []string{"caller cancel", "watch closed", "compacted", "malformed event", "slow consumer", "discovery close"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ch, err := f.d.Subscribe(ctx, "svc")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "caller cancel":
				cancel()
			case "watch closed":
				close(f.responses)
			case "compacted":
				f.responses <- clientv3.WatchResponse{CompactRevision: 11, Canceled: true}
			case "malformed event":
				f.responses <- clientv3.WatchResponse{Events: []*clientv3.Event{{Type: mvccpb.PUT, Kv: &mvccpb.KeyValue{Value: []byte("broken")}}}}
			case "slow consumer":
				events := make([]*clientv3.Event, 1000)
				for i := range events {
					events[i] = &clientv3.Event{Type: mvccpb.PUT, Kv: instanceKV(t, fmt.Sprint(i))}
				}
				f.responses <- clientv3.WatchResponse{Events: events}
				// Wait for cancellation before reading so the buffer must overflow.
				select {
				case <-f.watchCtx.Done():
				case <-time.After(2 * time.Second):
					t.Fatal("slow consumer was not disconnected")
				}
			case "discovery close":
				if err := f.d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			drained(t, ch)
		})
	}
}

func TestLargeReplay(t *testing.T) {
	f := newFixture(t)
	kvs := make([]*mvccpb.KeyValue, 1000)
	for i := range kvs {
		kvs[i] = instanceKV(t, fmt.Sprint(i))
	}
	f.kv.get = func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Header: &pb.ResponseHeader{Revision: 10}, Kvs: kvs}, nil
	}
	ch, err := f.d.Subscribe(t.Context(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range kvs {
		if ev := receive(t, ch); ev.Instance.ID != fmt.Sprint(i) {
			t.Fatalf("incomplete replay at %d", i)
		}
	}
}

func TestConcurrentCloseCancelsRequests(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	f.kv.put = func(ctx context.Context, _ string, _ string, _ ...clientv3.OpOption) (*clientv3.PutResponse, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	result := make(chan error, 1)
	go func() { result <- f.r.Register(context.Background(), &registry.ServiceInstance{Name: "svc"}) }()
	<-started
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := f.r.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight request not canceled: %v", err)
	}
	if n := f.lease.revokes.Load(); n != 1 {
		t.Fatalf("lease revoked %d times", n)
	}
	if f.r.client.Ctx().Err() != nil {
		t.Fatal("Close closed caller-owned client")
	}
	if err := f.r.Register(t.Context(), &registry.ServiceInstance{Name: "svc"}); !errors.Is(err, registry.ErrClosed) {
		t.Fatal(err)
	}
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.GetService(t.Context(), "svc"); !errors.Is(err, registry.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := f.d.Subscribe(t.Context(), "svc"); !errors.Is(err, registry.ErrClosed) {
		t.Fatal(err)
	}
}

func TestServicePrefixIsolation(t *testing.T) {
	prefix := serviceKey("svc")
	if !strings.HasSuffix(prefix, "/") || strings.HasPrefix(serviceKey("svc2"), prefix) {
		t.Fatalf("service prefix %q does not isolate svc from svc2", prefix)
	}
}

func TestDiscoveryNeedsNoLease(t *testing.T) {
	client := clientv3.NewCtxClient(t.Context())
	// No Lease implementation: discovery must only use KV and Watcher.
	client.KV = &testKV{get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
		return &clientv3.GetResponse{Header: &pb.ResponseHeader{Revision: 10}}, nil
	}}
	responses := make(chan clientv3.WatchResponse, 1)
	responses <- clientv3.WatchResponse{Created: true}
	client.Watcher = &testWatcher{watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
		return responses
	}}
	d, err := NewDiscovery(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close(); _ = client.Close() })
	if _, err := d.GetService(t.Context(), "svc"); err != nil {
		t.Fatal(err)
	}
	ch, err := d.Subscribe(t.Context(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	drained(t, ch)
	if client.Ctx().Err() != nil {
		t.Fatal("discovery closed the caller's client")
	}
}

func TestRegistryEndingDoesNotStopDiscovery(t *testing.T) {
	for _, reason := range []string{"close", "lease loss"} {
		t.Run(reason, func(t *testing.T) {
			f := newFixture(t)
			ch, err := f.d.Subscribe(t.Context(), "svc")
			if err != nil {
				t.Fatal(err)
			}
			if reason == "close" {
				if err := f.r.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				close(f.lease.expire)
				<-f.r.session.Ctx().Done()
			}
			if err := f.r.Register(t.Context(), &registry.ServiceInstance{Name: "svc"}); !errors.Is(err, registry.ErrClosed) {
				t.Fatalf("registration after lease ended: %v", err)
			}
			if _, err := f.d.GetService(t.Context(), "svc"); err != nil {
				t.Fatal(err)
			}
			f.responses <- clientv3.WatchResponse{Events: []*clientv3.Event{{Type: mvccpb.PUT, Kv: instanceKV(t, "other-process")}}}
			if ev := receive(t, ch); ev.Instance.ID != "other-process" {
				t.Fatalf("unexpected event: %+v", ev)
			}
			if err := f.d.Close(); err != nil {
				t.Fatal(err)
			}
			drained(t, ch)
		})
	}
}

func TestDiscoveryCloseDoesNotStopRegistration(t *testing.T) {
	f := newFixture(t)
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := f.r.Register(t.Context(), &registry.ServiceInstance{Name: "svc", ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if f.lease.grants.Load() != 1 || f.lease.revokes.Load() != 0 {
		t.Fatal("registrations must share one live lease")
	}
	if _, err := f.d.Subscribe(t.Context(), "svc"); !errors.Is(err, registry.ErrClosed) {
		t.Fatal(err)
	}
}

func TestCloseUsesBoundedCleanupContext(t *testing.T) {
	for _, revokeErr := range []error{rpctypes.ErrLeaseNotFound, errors.New("etcd unavailable")} {
		t.Run(revokeErr.Error(), func(t *testing.T) {
			f := newFixture(t)
			close(f.lease.expire)
			<-f.r.session.Ctx().Done()
			f.lease.revoke = func(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || ctx.Err() != nil || id != 123 {
					t.Fatalf("cleanup must have an independent bounded context: deadline=%v, err=%v, lease=%v", deadline, ctx.Err(), id)
				}
				return nil, revokeErr
			}
			want := revokeErr
			if errors.Is(revokeErr, rpctypes.ErrLeaseNotFound) {
				want = nil
			}
			for range 2 {
				if err := f.r.Close(); !errors.Is(err, want) {
					t.Fatalf("Close: got %v, want %v", err, want)
				}
			}
			if f.lease.revokes.Load() != 1 {
				t.Fatal("Close revoked the lease more than once")
			}
		})
	}
}
