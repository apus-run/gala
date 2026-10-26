package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/registry/registrytest"
)

func TestCloseDropsEverything(t *testing.T) {
	r := New()
	ins := &registry.ServiceInstance{Name: "svc", ID: "a"}
	if err := r.Register(context.Background(), ins); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if len(r.services) != 0 || len(r.subs) != 0 {
		t.Fatalf("state left after Close: %v %v", r.services, r.subs)
	}
	if err := r.Deregister(context.Background(), ins); err != nil {
		t.Fatalf("Deregister after Close should be a no-op, got %v", err)
	}
}

func TestSubscribeWithCancelledContext(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := r.Subscribe(ctx, "svc")
	if !errors.Is(err, context.Canceled) || ch != nil {
		t.Fatalf("got (%v, %v), want (nil, context.Canceled)", ch, err)
	}
}

func TestCancelledOperationsLeaveInstancesUnchanged(t *testing.T) {
	r := New()
	t.Cleanup(func() { _ = r.Close() })
	ins := &registry.ServiceInstance{Name: "svc", ID: "a"}
	if err := r.Register(t.Context(), ins); err != nil {
		t.Fatal(err)
	}
	ch, err := r.Subscribe(t.Context(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	<-ch // initial replay
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Deregister(ctx, ins); !errors.Is(err, context.Canceled) {
		t.Fatalf("Deregister: got %v, want context.Canceled", err)
	}
	if got, err := r.GetService(ctx, "svc"); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("GetService: got (%v, %v), want (nil, context.Canceled)", got, err)
	}
	got, err := r.GetService(t.Context(), "svc")
	if err != nil || len(got) != 1 || !got[0].Equal(ins) {
		t.Fatalf("cancelled operation changed instances: %v, %v", got, err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("cancelled operation published an event: %v", ev)
	default:
	}
}

func TestWithBufferSize(t *testing.T) {
	r := New(WithBufferSize(2))
	ctx := context.Background()
	ch, _ := r.Subscribe(ctx, "svc")
	for _, id := range []string{"a", "b", "c"} { // third overflows a buffer of 2
		_ = r.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: id})
	}
	n := 0
	for range ch {
		n++
	}
	if n != 2 {
		t.Fatalf("received %d events before the close, want 2", n)
	}
}

func TestMemoryRegistry(t *testing.T) {
	registrytest.Run(t, func(t *testing.T) (registry.Registry, registry.Discovery) {
		r := New()
		return r, r
	})
}
