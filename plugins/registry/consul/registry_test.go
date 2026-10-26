package consul

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/registry/registrytest"
)

const wait = 3 * time.Second

func newRegistryFor(t *testing.T, f *fakeConsul, opts ...Option) *Registry {
	t.Helper()
	base := []Option{WithAddress(f.url()), WithLogger(discardLogger()), WithTTL(6 * time.Second)}
	r, err := NewRegistry(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func newDiscoveryFor(t *testing.T, f *fakeConsul, opts ...Option) *Discovery {
	t.Helper()
	base := []Option{WithAddress(f.url()), WithLogger(discardLogger()), WithWaitTime(2 * time.Second)}
	d, err := NewDiscovery(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	d.opts.retryMin, d.opts.retryMax, d.opts.minPoll = 5*time.Millisecond, 20*time.Millisecond, 5*time.Millisecond
	return d
}

func newPair(t *testing.T) (*fakeConsul, *Registry, *Discovery) {
	t.Helper()
	f := newFakeConsul(t)
	r, d := newRegistryFor(t, f), newDiscoveryFor(t, f)
	t.Cleanup(func() {
		_ = r.Close()
		_ = d.Close()
	})
	return f, r, d
}

func inst(name, id string) *registry.ServiceInstance {
	return &registry.ServiceInstance{
		Name: name, ID: id, Version: "v1",
		Metadata:  map[string]string{"zone": "a"},
		Endpoints: []string{"grpc://127.0.0.1:9000?isSecure=false", "http://127.0.0.1:8000"},
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func recv(t *testing.T, ch <-chan registry.Event) registry.Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, expected an event")
		}
		return ev
	case <-time.After(wait):
		t.Fatal("timed out waiting for an event")
		return registry.Event{}
	}
}

func expectClosed(t *testing.T, ch <-chan registry.Event) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel was not closed")
		}
	}
}

func expectNoEvent(t *testing.T, ch <-chan registry.Event) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		t.Fatalf("unexpected event %v %v (open=%v)", ev.Type, ev.Instance, ok)
	case <-time.After(60 * time.Millisecond):
	}
}

func TestContract(t *testing.T) {
	registrytest.Run(t, func(t *testing.T) (registry.Registry, registry.Discovery) {
		f := newFakeConsul(t)
		return newRegistryFor(t, f), newDiscoveryFor(t, f)
	})
}

func TestRegisterSendsWhatConsulExpects(t *testing.T) {
	f := newFakeConsul(t)
	r := newRegistryFor(t, f, WithToken("s3cret"), WithDatacenter("dc9"),
		WithTTL(10*time.Second), WithDeregisterCriticalAfter(2*time.Minute))
	defer r.Close()

	ins := inst("student-api", "i-1")
	if err := r.Register(context.Background(), ins); err != nil {
		t.Fatal(err)
	}

	svc, ok := f.service("student-api/i-1")
	if !ok {
		t.Fatalf("service not registered; have %v", f.serviceIDs())
	}
	if svc.Name != "student-api" || svc.Address != "127.0.0.1" || svc.Port != 9000 {
		t.Fatalf("name/address/port = %q %q %d, want student-api 127.0.0.1 9000", svc.Name, svc.Address, svc.Port)
	}
	if svc.Meta["gala.id"] != "i-1" || svc.Meta["gala.version"] != "v1" || svc.Meta["zone"] != "a" {
		t.Fatalf("meta = %v", svc.Meta)
	}
	if want := `["grpc://127.0.0.1:9000?isSecure=false","http://127.0.0.1:8000"]`; svc.Meta["gala.endpoints"] != want {
		t.Fatalf("gala.endpoints = %s, want %s", svc.Meta["gala.endpoints"], want)
	}
	if svc.CheckID != "service:student-api/i-1" || svc.TTL != "10s" || svc.DeregisterAfter != "2m0s" || svc.Status != "passing" {
		t.Fatalf("check = %+v", svc)
	}

	// Every request carries the token and datacenter; registration replaces old checks.
	var sawRegister bool
	for _, req := range f.requests() {
		if req.Token != "s3cret" || req.Query.Get("dc") != "dc9" {
			t.Fatalf("request %s %s lacks token or dc: %+v", req.Method, req.Path, req)
		}
		if req.Path == "/v1/agent/service/register" {
			sawRegister = true
			if req.Query.Get("replace-existing-checks") != "true" {
				t.Fatalf("register query = %v", req.Query)
			}
		}
	}
	if !sawRegister {
		t.Fatal("no register request seen")
	}
}

func TestMetadataWithReservedPrefixIsRejected(t *testing.T) {
	f, r, _ := newPair(t)
	ins := inst("svc", "a")
	ins.Metadata["gala.id"] = "spoofed"
	err := r.Register(context.Background(), ins)
	if !errors.Is(err, ErrReservedMetadata) {
		t.Fatalf("err = %v, want ErrReservedMetadata", err)
	}
	if len(f.serviceIDs()) != 0 {
		t.Fatalf("a rejected instance was registered: %v", f.serviceIDs())
	}
}

func TestEndpointsTooLongForMetaAreRejected(t *testing.T) {
	_, r, _ := newPair(t)
	ins := inst("svc", "a")
	for range 30 {
		ins.Endpoints = append(ins.Endpoints, "grpc://a-rather-long-host-name.example.internal:9000")
	}
	if err := r.Register(context.Background(), ins); err == nil {
		t.Fatal("expected an error for endpoints beyond Consul's meta value limit")
	}
}

func TestKeepAlivePassesTheCheck(t *testing.T) {
	f := newFakeConsul(t)
	r := newRegistryFor(t, f)
	defer r.Close()
	r.opts.checkInterval = 10 * time.Millisecond

	if err := r.Register(context.Background(), inst("svc", "a")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "check passed repeatedly", func() bool { return f.passCount("service:svc/a") >= 3 })
}

func TestDefaultCheckIntervalIsAThirdOfTheTTL(t *testing.T) {
	o, err := newOptions([]Option{WithTTL(30 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if got := o.interval(); got != 10*time.Second {
		t.Fatalf("interval = %v, want 10s", got)
	}
}

func TestRegistersAgainWhenConsulForgetsTheService(t *testing.T) {
	f := newFakeConsul(t)
	r := newRegistryFor(t, f)
	defer r.Close()
	r.opts.checkInterval = 10 * time.Millisecond

	if err := r.Register(context.Background(), inst("svc", "a")); err != nil {
		t.Fatal(err)
	}
	f.forget("svc/a") // agent restart without persisted state

	eventually(t, "service registered again", func() bool {
		s, ok := f.service("svc/a")
		return ok && s.Status == "passing"
	})
}

func TestCriticalInstanceDisappearsAndComesBack(t *testing.T) {
	f, r, d := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Register(ctx, inst("svc", "a")); err != nil {
		t.Fatal(err)
	}
	ch, err := d.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if ev := recv(t, ch); ev.Type != registry.EventTypeAdd || ev.Instance.ID != "a" {
		t.Fatalf("replay = %v %v", ev.Type, ev.Instance)
	}

	f.setStatus("svc/a", "critical") // the process stopped passing its check
	if ev := recv(t, ch); ev.Type != registry.EventTypeDelete || ev.Instance.ID != "a" {
		t.Fatalf("got %v %v, want Delete a", ev.Type, ev.Instance)
	}
	got, err := d.GetService(ctx, "svc")
	if err != nil || len(got) != 0 {
		t.Fatalf("GetService = %v, %v; an unhealthy instance must not be returned", got, err)
	}

	f.setStatus("svc/a", "passing")
	if ev := recv(t, ch); ev.Type != registry.EventTypeAdd || ev.Instance.ID != "a" {
		t.Fatalf("got %v %v, want Add a", ev.Type, ev.Instance)
	}
}

func TestOnlyPassingInstancesAreQueried(t *testing.T) {
	f, r, d := newPair(t)
	ctx := context.Background()
	_ = r.Register(ctx, inst("svc", "a"))
	if _, err := d.GetService(ctx, "svc"); err != nil {
		t.Fatal(err)
	}
	var sawQuery bool
	for _, req := range f.requests() {
		if req.Path == "/v1/health/service/svc" {
			sawQuery = true
			if req.Query.Get("passing") != "true" {
				t.Fatalf("health query lacks passing=true: %v", req.Query)
			}
		}
	}
	if !sawQuery {
		t.Fatal("no health query seen")
	}
}

func TestWatchUsesBlockingQueries(t *testing.T) {
	f, r, d := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = r.Register(ctx, inst("svc", "a"))
	ch, _ := d.Subscribe(ctx, "svc")
	recv(t, ch)
	_ = r.Register(ctx, inst("svc", "b"))
	recv(t, ch)

	eventually(t, "a blocking query with index and wait", func() bool {
		for _, req := range f.requests() {
			if req.Path == "/v1/health/service/svc" && req.Query.Get("index") != "" && req.Query.Get("wait") == "2s" {
				return true
			}
		}
		return false
	})
}

func TestUpdatedInstanceProducesAdd(t *testing.T) {
	_, r, d := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ins := inst("svc", "a")
	_ = r.Register(ctx, ins)
	ch, _ := d.Subscribe(ctx, "svc")
	recv(t, ch)

	ins.Version = "v2"
	if err := r.Register(ctx, ins); err != nil {
		t.Fatal(err)
	}
	ev := recv(t, ch)
	if ev.Type != registry.EventTypeAdd || ev.Instance.Version != "v2" {
		t.Fatalf("got %v %+v, want Add with v2", ev.Type, ev.Instance)
	}
}

func TestUnchangedPollsProduceNoEvents(t *testing.T) {
	f, r, d := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = r.Register(ctx, inst("svc", "a"))
	ch, _ := d.Subscribe(ctx, "svc")
	recv(t, ch)

	// Changes to other services bump Consul's index; the watcher wakes up,
	// sees the same instances and must stay quiet.
	f.addService(fakeService{ID: "other-1", Name: "other"})
	f.addService(fakeService{ID: "other-2", Name: "other"})
	expectNoEvent(t, ch)
}

func TestDeregisterRemovesTheService(t *testing.T) {
	f, r, _ := newPair(t)
	ctx := context.Background()
	ins := inst("svc", "a")
	_ = r.Register(ctx, ins)
	if err := r.Deregister(ctx, ins); err != nil {
		t.Fatal(err)
	}
	if ids := f.serviceIDs(); len(ids) != 0 {
		t.Fatalf("still registered: %v", ids)
	}
	// Idempotent: Consul answers 404 the second time.
	if err := r.Deregister(ctx, ins); err != nil {
		t.Fatalf("second Deregister: %v", err)
	}
}

func TestDeregisterStopsKeepAlive(t *testing.T) {
	f := newFakeConsul(t)
	r := newRegistryFor(t, f)
	defer r.Close()
	r.opts.checkInterval = 5 * time.Millisecond
	ctx := context.Background()
	ins := inst("svc", "a")
	_ = r.Register(ctx, ins)
	_ = r.Deregister(ctx, ins)

	before := f.passCount("service:svc/a")
	time.Sleep(60 * time.Millisecond)
	if after := f.passCount("service:svc/a"); after != before {
		t.Fatalf("check still being passed after Deregister: %d -> %d", before, after)
	}
	if len(f.serviceIDs()) != 0 {
		t.Fatalf("service came back: %v", f.serviceIDs())
	}
}

func TestDeregisterRemovesUntrackedService(t *testing.T) {
	f, r, _ := newPair(t)
	f.addService(fakeService{ID: "svc/old", Name: "svc"}) // from an earlier run
	if err := r.Deregister(context.Background(), inst("svc", "old")); err != nil {
		t.Fatal(err)
	}
	if len(f.serviceIDs()) != 0 {
		t.Fatalf("not removed: %v", f.serviceIDs())
	}
}

func TestCloseDeregistersEverything(t *testing.T) {
	f, r, _ := newPair(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := r.Register(context.Background(), inst("svc", id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if ids := f.serviceIDs(); len(ids) != 0 {
		t.Fatalf("left after Close: %v", ids)
	}
}

func TestRegisterAgainReplacesTheInstance(t *testing.T) {
	f, r, _ := newPair(t)
	ins := inst("svc", "a")
	_ = r.Register(context.Background(), ins)
	ins.Metadata = map[string]string{"zone": "b"}
	if err := r.Register(context.Background(), ins); err != nil {
		t.Fatal(err)
	}
	if ids := f.serviceIDs(); len(ids) != 1 {
		t.Fatalf("services = %v, want exactly one", ids)
	}
	if s, _ := f.service("svc/a"); s.Meta["zone"] != "b" {
		t.Fatalf("meta = %v", s.Meta)
	}
}

func TestRegisterFailureIsReported(t *testing.T) {
	f, r, _ := newPair(t)
	f.setDown(true)
	if err := r.Register(context.Background(), inst("svc", "a")); err == nil {
		t.Fatal("expected an error while Consul is failing")
	}
	f.setDown(false)
	time.Sleep(30 * time.Millisecond)
	if len(f.serviceIDs()) != 0 {
		t.Fatalf("a failed Register must not register later: %v", f.serviceIDs())
	}
}

func TestSubscriptionSurvivesAnOutage(t *testing.T) {
	f, r, d := newPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = r.Register(ctx, inst("svc", "a"))
	ch, err := d.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch)

	f.setDown(true)
	eventually(t, "polls failing", func() bool { return f.failureCount() >= 3 })
	f.addService(fakeService{ // registered while the watcher was blind
		ID: "svc-b", Name: "svc",
		Meta: map[string]string{"gala.id": "b", "gala.endpoints": `["grpc://10.0.0.2:9000"]`},
	})
	f.setDown(false)

	ev := recv(t, ch)
	if ev.Type != registry.EventTypeAdd || ev.Instance.ID != "b" {
		t.Fatalf("got %v %v, want Add b after recovery", ev.Type, ev.Instance)
	}
}

func TestUnrecoverableWatchErrorClosesTheSubscription(t *testing.T) {
	f, r, d := newPair(t)
	_ = r.Register(context.Background(), inst("svc", "a"))
	ch, err := d.Subscribe(context.Background(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	recv(t, ch)

	f.setDeny(true) // e.g. the ACL token was revoked
	expectClosed(t, ch)
	if _, err := d.Subscribe(context.Background(), "svc"); err == nil {
		t.Fatal("resubscribing with a denied token should report the error")
	}
}

func TestForeignServicesAreDiscovered(t *testing.T) {
	f, _, d := newPair(t)
	f.addService(fakeService{
		ID: "legacy-1", Name: "legacy", Address: "10.1.2.3", Port: 8080,
		Meta: map[string]string{"team": "core"},
	})
	f.addService(fakeService{ID: "legacy-2", Name: "legacy", Port: 9090}) // no address: falls back to the node

	got, err := d.GetService(context.Background(), "legacy")
	if err != nil || len(got) != 2 {
		t.Fatalf("GetService = %v, %v", got, err)
	}
	if got[0].ID != "legacy-1" || !slices.Equal(got[0].Endpoints, []string{"tcp://10.1.2.3:8080"}) || got[0].Metadata["team"] != "core" {
		t.Fatalf("first = %+v", got[0])
	}
	if !slices.Equal(got[1].Endpoints, []string{"tcp://10.9.9.9:9090"}) {
		t.Fatalf("second endpoints = %v, want the node address", got[1].Endpoints)
	}
}

func TestDiffIgnoresEqualInstances(t *testing.T) {
	a := &registry.ServiceInstance{Name: "svc", ID: "x", Version: "1"}
	events := diff(map[string]*registry.ServiceInstance{"x": a}, []*registry.ServiceInstance{a.Clone()})
	if len(events) != 0 {
		t.Fatalf("an equal instance produced events: %v", events)
	}
}

func TestDiffOrdersDeletesBeforeAdds(t *testing.T) {
	last := map[string]*registry.ServiceInstance{
		"b": {Name: "s", ID: "b"}, "a": {Name: "s", ID: "a"}, "keep": {Name: "s", ID: "keep"},
	}
	current := []*registry.ServiceInstance{
		{Name: "s", ID: "keep"}, {Name: "s", ID: "n1"}, {Name: "s", ID: "n2"},
	}
	var got []string
	for _, ev := range diff(last, current) {
		got = append(got, ev.Type.String()+":"+ev.Instance.ID)
	}
	want := []string{"delete:a", "delete:b", "add:n1", "add:n2"}
	if !slices.Equal(got, want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
}

func TestNextIndex(t *testing.T) {
	tests := []struct{ prev, got, want uint64 }{
		{0, 5, 5}, // first answer
		{5, 9, 9}, // normal progress
		{5, 5, 5}, // wait elapsed, nothing changed
		{9, 3, 0}, // went backwards: reset (Consul docs)
		{5, 0, 0}, // missing or zero index: reset
		{0, 0, 1},
	}
	for _, tt := range tests {
		if got := nextIndex(tt.prev, tt.got); got != tt.want {
			t.Errorf("nextIndex(%d, %d) = %d, want %d", tt.prev, tt.got, got, tt.want)
		}
	}
}

func TestAddressNormalization(t *testing.T) {
	tests := []struct {
		in, want string
		bad      bool
	}{
		{"consul.local:8500", "http://consul.local:8500", false},
		{"https://consul.local:8501/", "https://consul.local:8501", false},
		{"http://10.0.0.1:8500", "http://10.0.0.1:8500", false},
		{"ftp://x", "", true},
		{"http://", "", true},
	}
	for _, tt := range tests {
		got, err := normalizeAddress(tt.in)
		if (err != nil) != tt.bad || got != tt.want {
			t.Errorf("normalizeAddress(%q) = %q, %v; want %q (bad=%v)", tt.in, got, err, tt.want, tt.bad)
		}
	}
	if _, err := NewRegistry(WithAddress("ftp://x")); err == nil {
		t.Error("NewRegistry accepted a bad address")
	}
	if _, err := NewDiscovery(WithAddress("ftp://x")); err == nil {
		t.Error("NewDiscovery accepted a bad address")
	}
}

func TestOptionsAreSanitized(t *testing.T) {
	o, err := newOptions([]Option{
		WithTTL(10 * time.Millisecond), WithBufferSize(-1), WithWaitTime(-1),
		WithRequestTimeout(0), WithDeregisterCriticalAfter(-time.Hour),
		WithLogger(nil), WithHTTPClient(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.ttl != time.Second || o.bufferSize != defaultBufferSize || o.wait != defaultWait ||
		o.requestTimeout != defaultRequestTimeout || o.deregisterAfter != 0 ||
		o.logger == nil || o.httpClient == nil {
		t.Fatalf("options = %+v", o)
	}
	if got := secs(1500 * time.Millisecond); got != "2s" {
		t.Fatalf("secs(1.5s) = %q, want 2s (rounded up)", got)
	}
}

func TestServiceIDDoesNotCollide(t *testing.T) {
	f, r, d := newPair(t)
	for _, ins := range []*registry.ServiceInstance{inst("a-b", "c"), inst("a", "b-c")} {
		if err := r.Register(t.Context(), ins); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.serviceIDs()) != 2 {
		t.Fatalf("services collided: %v", f.serviceIDs())
	}
	if err := r.Deregister(t.Context(), inst("a-b", "c")); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetService(t.Context(), "a")
	if err != nil || len(got) != 1 || got[0].ID != "b-c" {
		t.Fatalf("other service affected: %v, %v", got, err)
	}
}

func TestDiscoveryCloseCancelsInitialRequests(t *testing.T) {
	for _, subscribe := range []bool{false, true} {
		t.Run(map[bool]string{false: "get", true: "subscribe"}[subscribe], func(t *testing.T) {
			started := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
			}))
			defer srv.Close()
			d, err := NewDiscovery(WithAddress(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			result := make(chan error, 1)
			go func() {
				if subscribe {
					_, err := d.Subscribe(context.Background(), "svc")
					result <- err
				} else {
					_, err := d.GetService(context.Background(), "svc")
					result <- err
				}
			}()
			<-started
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want cancellation", err)
			}
		})
	}
}

func TestRegistryCloseWaitsForInflightRegistration(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/agent/service/register" {
			_, _ = io.Copy(io.Discard, r.Body)
			close(started)
			<-r.Context().Done()
		}
	}))
	defer srv.Close()
	r, err := NewRegistry(WithAddress(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	result := make(chan error, 1)
	go func() { result <- r.Register(context.Background(), inst("svc", "a")) }()
	<-started
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	if len(r.entries) != 0 {
		t.Fatal("registration left after Close")
	}
}

func TestRolesHaveIndependentLifetimes(t *testing.T) {
	for _, closeDiscovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "registry", true: "discovery"}[closeDiscovery], func(t *testing.T) {
			_, r, d := newPair(t)
			if closeDiscovery {
				_ = d.Close()
				if err := r.Register(t.Context(), inst("svc", "a")); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = r.Close()
				if _, err := d.GetService(t.Context(), "svc"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRegisterRetryKeepsGeneratedID(t *testing.T) {
	f, r, _ := newPair(t)
	ins := inst("svc", "")
	f.setDown(true)
	if err := r.Register(t.Context(), ins); err == nil {
		t.Fatal("expected failure")
	}
	id := ins.ID
	if id == "" {
		t.Fatal("missing generated ID")
	}
	f.setDown(false)
	if err := r.Register(t.Context(), ins); err != nil {
		t.Fatal(err)
	}
	if ins.ID != id {
		t.Fatal("retry changed ID")
	}
}

// fakeConsul is an httptest server that speaks the subset of the Consul
// agent and health HTTP API this plugin uses, including blocking queries. Its
// wire structs are written separately from the plugin's, so a mistake in the
// plugin's JSON field names or paths is caught instead of mirrored.
type fakeConsul struct {
	srv *httptest.Server

	mu       sync.Mutex
	index    uint64
	changed  chan struct{} // closed and replaced on every change
	services map[string]*fakeService
	passes   map[string]int // check ID -> successful passes
	reqs     []fakeReq
	down     bool // answer 500
	deny     bool // answer 403
	failures int  // requests answered with an injected error
}

type fakeService struct {
	ID, Name, Address string
	Port              int
	Meta              map[string]string
	CheckID, TTL      string
	DeregisterAfter   string
	Status            string // passing, warning, critical
}

type fakeReq struct {
	Method, Path string
	Query        url.Values
	Token        string
}

func newFakeConsul(t *testing.T) *fakeConsul {
	t.Helper()
	f := &fakeConsul{
		index:    1,
		changed:  make(chan struct{}),
		services: make(map[string]*fakeService),
		passes:   make(map[string]int),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeConsul) url() string { return f.srv.URL }

// --- HTTP ------------------------------------------------------------------

func (f *fakeConsul) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.reqs = append(f.reqs, fakeReq{r.Method, r.URL.Path, r.URL.Query(), r.Header.Get("X-Consul-Token")})
	down, deny := f.down, f.deny
	if down || deny {
		f.failures++
	}
	f.mu.Unlock()
	switch {
	case down:
		http.Error(w, "injected failure", http.StatusInternalServerError)
		return
	case deny:
		http.Error(w, "ACL not found", http.StatusForbidden)
		return
	}

	const (
		register   = "/v1/agent/service/register"
		deregister = "/v1/agent/service/deregister/"
		pass       = "/v1/agent/check/pass/"
		health     = "/v1/health/service/"
	)
	switch {
	case r.Method == http.MethodPut && r.URL.Path == register:
		f.handleRegister(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, deregister):
		f.handleDeregister(w, strings.TrimPrefix(r.URL.Path, deregister))
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, pass):
		f.handlePass(w, strings.TrimPrefix(r.URL.Path, pass))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, health):
		f.handleHealth(w, r, strings.TrimPrefix(r.URL.Path, health))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeConsul) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID, Name, Address string
		Port              int
		Meta              map[string]string
		Check             *struct {
			CheckID, Name, TTL, Status, DeregisterCriticalServiceAfter string
		}
	}
	b, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(b, &body); err != nil || body.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.ID == "" {
		body.ID = body.Name
	}
	svc := &fakeService{
		ID: body.ID, Name: body.Name, Address: body.Address, Port: body.Port,
		Meta: body.Meta, Status: "critical", // Consul's default without an explicit status
	}
	if c := body.Check; c != nil {
		svc.CheckID, svc.TTL, svc.DeregisterAfter = c.CheckID, c.TTL, c.DeregisterCriticalServiceAfter
		if c.Status != "" {
			svc.Status = c.Status
		}
	}
	f.mu.Lock()
	f.services[svc.ID] = svc
	f.bumpLocked()
	f.mu.Unlock()
}

func (f *fakeConsul) handleDeregister(w http.ResponseWriter, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.services[id]; !ok {
		http.Error(w, "Unknown service ID", http.StatusNotFound)
		return
	}
	delete(f.services, id)
	f.bumpLocked()
}

func (f *fakeConsul) handlePass(w http.ResponseWriter, checkID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.services {
		if s.CheckID == checkID {
			f.passes[checkID]++
			if s.Status != "passing" {
				s.Status = "passing"
				f.bumpLocked()
			}
			return
		}
	}
	http.Error(w, "Unknown check ID", http.StatusNotFound)
}

type fakeHealthEntry struct {
	Node struct {
		Node, Address string
	}
	Service struct {
		ID, Service, Address string
		Port                 int
		Meta                 map[string]string
	}
	Checks []struct{ CheckID, Status string }
}

func (f *fakeConsul) handleHealth(w http.ResponseWriter, r *http.Request, name string) {
	q := r.URL.Query()
	passing := q.Get("passing") == "true"
	var index uint64
	if v := q.Get("index"); v != "" {
		index, _ = strconv.ParseUint(v, 10, 64)
	}
	wait := 30 * time.Second
	if v := q.Get("wait"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			wait = d
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	for {
		f.mu.Lock()
		if f.down {
			f.failures++
			f.mu.Unlock()
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		if index == 0 || f.index > index {
			f.respondLocked(w, name, passing)
			return
		}
		changed := f.changed
		f.mu.Unlock()

		select {
		case <-changed:
		case <-timer.C: // wait elapsed: answer with the unchanged data
			f.mu.Lock()
			f.respondLocked(w, name, passing)
			return
		case <-r.Context().Done():
			return
		}
	}
}

// respondLocked writes the answer and releases f.mu.
func (f *fakeConsul) respondLocked(w http.ResponseWriter, name string, passing bool) {
	var entries []fakeHealthEntry
	for _, s := range f.services {
		if s.Name != name || (passing && s.Status != "passing") {
			continue
		}
		var e fakeHealthEntry
		e.Node.Node, e.Node.Address = "node-1", "10.9.9.9"
		e.Service.ID, e.Service.Service = s.ID, s.Name
		e.Service.Address, e.Service.Port, e.Service.Meta = s.Address, s.Port, s.Meta
		e.Checks = []struct{ CheckID, Status string }{{s.CheckID, s.Status}}
		entries = append(entries, e)
	}
	index := f.index
	f.mu.Unlock()

	slices.SortFunc(entries, func(a, b fakeHealthEntry) int { return strings.Compare(a.Service.ID, b.Service.ID) })
	if entries == nil {
		entries = []fakeHealthEntry{}
	}
	w.Header().Set("X-Consul-Index", strconv.FormatUint(index, 10))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

func (f *fakeConsul) bumpLocked() {
	f.index++
	close(f.changed)
	f.changed = make(chan struct{})
}

// --- helpers used by tests --------------------------------------------------

// addService adds or replaces a service directly, as another tool would.
func (f *fakeConsul) addService(s fakeService) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.Status == "" {
		s.Status = "passing"
	}
	f.services[s.ID] = &s
	f.bumpLocked()
}

func (f *fakeConsul) setStatus(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services[id].Status = status
	f.bumpLocked()
}

// forget drops a service without a trace, like an agent that restarted
// without persisting its state.
func (f *fakeConsul) forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.services, id)
	f.bumpLocked()
}

func (f *fakeConsul) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
	f.bumpLocked() // wakes blocked queries so they notice
}

func (f *fakeConsul) setDeny(deny bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deny = deny
	f.bumpLocked()
}

func (f *fakeConsul) service(id string) (fakeService, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.services[id]
	if !ok {
		return fakeService{}, false
	}
	return *s, true
}

func (f *fakeConsul) serviceIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.services))
	for id := range f.services {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (f *fakeConsul) passCount(checkID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.passes[checkID]
}

func (f *fakeConsul) failureCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failures
}

func (f *fakeConsul) requests() []fakeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
