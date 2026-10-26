// Package registrytest provides the shared registration and discovery contract tests.
package registrytest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apus-run/gala/registry"
)

// NewFunc builds a fresh, empty registry for one test. The suite closes both
// halves when the test ends.
type NewFunc func(t *testing.T) (registry.Registry, registry.Discovery)

const wait = 3 * time.Second

// Run runs the whole suite.
func Run(t *testing.T, newFn NewFunc) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(*testing.T, registry.Registry, registry.Discovery)
	}{
		{"RegisterAndGetService", testRegisterAndGet},
		{"RegisterGeneratesID", testRegisterGeneratesID},
		{"InvalidArguments", testInvalidArguments},
		{"ReturnedInstancesAreIndependent", testIndependentCopies},
		{"SubscribeReplaysThenStreams", testSubscribeReplayThenStream},
		{"RegisterTwiceUpserts", testRegisterTwiceUpserts},
		{"DeregisterUnknownIsNoop", testDeregisterUnknown},
		{"ContextCancelClosesChannel", testContextCancel},
		{"DiscoveryCloseClosesSubscriptions", testDiscoveryClose},
		{"SlowConsumerIsClosedNotBlocking", testSlowConsumer},
		{"RegistryClose", testRegistryClose},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, dis := newFn(t)
			t.Cleanup(func() {
				_ = reg.Close()
				_ = dis.Close()
			})
			tt.fn(t, reg, dis)
		})
	}
}

func inst(name, id string) *registry.ServiceInstance {
	return &registry.ServiceInstance{
		Name:      name,
		ID:        id,
		Version:   "v1",
		Metadata:  map[string]string{"zone": "a"},
		Endpoints: []string{"grpc://127.0.0.1:9000", "http://127.0.0.1:8000"},
	}
}

func mustRegister(t *testing.T, r registry.Registry, ins *registry.ServiceInstance) {
	t.Helper()
	if err := r.Register(context.Background(), ins); err != nil {
		t.Fatalf("Register(%s/%s): %v", ins.Name, ins.ID, err)
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

// expectClosed drains ch and fails unless it is closed in time.
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

func expectEvent(t *testing.T, ch <-chan registry.Event, typ registry.EventType, id string) registry.Event {
	t.Helper()
	ev := recv(t, ch)
	if ev.Type != typ || ev.Instance == nil || ev.Instance.ID != id {
		t.Fatalf("got %v %v, want %v %q", ev.Type, ev.Instance, typ, id)
	}
	return ev
}

func testRegisterAndGet(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx := context.Background()
	a1, a2 := inst("svc", "a1"), inst("svc", "a2")
	mustRegister(t, reg, a1)
	mustRegister(t, reg, a2)
	// Names sharing a prefix must stay isolated.
	mustRegister(t, reg, inst("svc2", "b1"))
	mustRegister(t, reg, inst("sv", "c1"))

	got, err := dis.GetService(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d instances, want 2: %v", len(got), got)
	}
	byID := map[string]*registry.ServiceInstance{}
	for _, ins := range got {
		byID[ins.ID] = ins
	}
	if !a1.Equal(byID["a1"]) || !a2.Equal(byID["a2"]) {
		t.Fatalf("instances differ from what was registered: %v", got)
	}

	none, err := dis.GetService(ctx, "missing")
	if err != nil || len(none) != 0 {
		t.Fatalf("GetService(missing) = %v, %v; want empty, nil", none, err)
	}
}

func testRegisterGeneratesID(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ins := inst("svc", "")
	mustRegister(t, reg, ins)
	if ins.ID == "" {
		t.Fatal("Register did not write the generated ID back")
	}
	got, err := dis.GetService(context.Background(), "svc")
	if err != nil || len(got) != 1 || got[0].ID != ins.ID {
		t.Fatalf("GetService = %v, %v; want the instance with ID %q", got, err, ins.ID)
	}
}

func testInvalidArguments(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx := context.Background()
	var nilIns *registry.ServiceInstance

	check := func(what string, err, want error) {
		t.Helper()
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", what, err, want)
		}
	}
	check("Register(nil)", reg.Register(ctx, nilIns), registry.ErrNilInstance)
	check("Register(empty name)", reg.Register(ctx, inst("", "x")), registry.ErrInvalidName)
	check("Register(name with /)", reg.Register(ctx, inst("a/b", "x")), registry.ErrInvalidName)
	check("Register(id with /)", reg.Register(ctx, inst("a", "x/y")), registry.ErrInvalidID)
	check("Deregister(nil)", reg.Deregister(ctx, nilIns), registry.ErrNilInstance)
	check("Deregister(empty id)", reg.Deregister(ctx, inst("a", "")), registry.ErrInvalidID)

	_, err := dis.GetService(ctx, "a/b")
	check("GetService(a/b)", err, registry.ErrInvalidName)
	_, err = dis.Subscribe(ctx, "")
	check("Subscribe(empty)", err, registry.ErrInvalidName)
}

func testIndependentCopies(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx := context.Background()
	mustRegister(t, reg, inst("svc", "a"))

	got, _ := dis.GetService(ctx, "svc")
	got[0].Metadata["zone"] = "mutated"
	got[0].Endpoints[0] = "mutated"

	again, _ := dis.GetService(ctx, "svc")
	if !inst("svc", "a").Equal(again[0]) {
		t.Fatalf("mutating a result changed the registry: %+v", again[0])
	}
}

func testSubscribeReplayThenStream(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mustRegister(t, reg, inst("svc", "a"))
	mustRegister(t, reg, inst("svc", "b"))
	mustRegister(t, reg, inst("other", "x")) // must not show up

	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	replay := map[string]bool{}
	for range 2 {
		ev := recv(t, ch)
		if ev.Type != registry.EventTypeAdd {
			t.Fatalf("replay event %v, want Add", ev.Type)
		}
		replay[ev.Instance.ID] = true
	}
	if !replay["a"] || !replay["b"] {
		t.Fatalf("replay = %v, want a and b", replay)
	}

	c := inst("svc", "c")
	mustRegister(t, reg, c)
	ev := expectEvent(t, ch, registry.EventTypeAdd, "c")
	if ev.Instance.Name != "svc" || len(ev.Instance.Endpoints) != 2 {
		t.Fatalf("Add event lost fields: %+v", ev.Instance)
	}

	if err := reg.Deregister(ctx, c); err != nil {
		t.Fatal(err)
	}
	ev = expectEvent(t, ch, registry.EventTypeDelete, "c")
	if ev.Instance.Name != "svc" {
		t.Fatalf("Delete event Name = %q, want svc", ev.Instance.Name)
	}
}

func testRegisterTwiceUpserts(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ins := inst("svc", "a")
	mustRegister(t, reg, ins)

	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	expectEvent(t, ch, registry.EventTypeAdd, "a")

	ins.Metadata = map[string]string{"zone": "b"}
	mustRegister(t, reg, ins)
	ev := expectEvent(t, ch, registry.EventTypeAdd, "a")
	if ev.Instance.Metadata["zone"] != "b" {
		t.Fatalf("update event carries stale metadata: %v", ev.Instance.Metadata)
	}

	got, _ := dis.GetService(ctx, "svc")
	if len(got) != 1 || got[0].Metadata["zone"] != "b" {
		t.Fatalf("GetService = %v, want the single updated instance", got)
	}
}

func testDeregisterUnknown(t *testing.T, reg registry.Registry, _ registry.Discovery) {
	if err := reg.Deregister(context.Background(), inst("svc", "never-registered")); err != nil {
		t.Fatalf("Deregister of unknown instance: %v", err)
	}
}

func testContextCancel(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	mustRegister(t, reg, inst("svc", "a"))
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	expectEvent(t, ch, registry.EventTypeAdd, "a")
	cancel()
	expectClosed(t, ch)
}

func testDiscoveryClose(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx := context.Background()
	mustRegister(t, reg, inst("svc", "a"))
	ch1, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}

	if err := dis.Close(); err != nil {
		t.Fatal(err)
	}
	expectClosed(t, ch1)
	expectClosed(t, ch2)

	if err := dis.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := dis.Subscribe(ctx, "svc"); !errors.Is(err, registry.ErrClosed) {
		t.Fatalf("Subscribe after Close: err = %v, want ErrClosed", err)
	}
	if _, err := dis.GetService(ctx, "svc"); !errors.Is(err, registry.ErrClosed) {
		t.Fatalf("GetService after Close: err = %v, want ErrClosed", err)
	}
}

func testSlowConsumer(t *testing.T, reg registry.Registry, dis registry.Discovery) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}

	// Never read from ch while registering far more than any sane buffer.
	// The Registry must not block, and the subscription must end.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 1000 {
			if err := reg.Register(ctx, inst("svc", fmt.Sprintf("i-%04d", i))); err != nil {
				t.Errorf("Register #%d: %v", i, err)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Register blocked behind a slow subscriber")
	}

	// Asynchronous backends forward events from a goroutine; give it time to
	// overflow the unread subscription before the reader starts draining.
	time.Sleep(100 * time.Millisecond)

	received := 0
	deadline := time.After(wait)
loop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break loop
			}
			received++
		case <-deadline:
			t.Fatal("slow consumer's channel was never closed")
		}
	}
	if received >= 1000 {
		t.Fatalf("received all %d events; expected the subscription to overflow", received)
	}

	// The consumer's recovery path: resubscribe and see the full state.
	ch, err = dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	for range 1000 {
		if ev := recv(t, ch); ev.Type != registry.EventTypeAdd {
			t.Fatalf("replay event %v, want Add", ev.Type)
		}
	}
}

func testRegistryClose(t *testing.T, reg registry.Registry, _ registry.Discovery) {
	mustRegister(t, reg, inst("svc", "a"))
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reg.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := reg.Register(context.Background(), inst("svc", "b")); !errors.Is(err, registry.ErrClosed) {
		t.Fatalf("Register after Close: err = %v, want ErrClosed", err)
	}
}
