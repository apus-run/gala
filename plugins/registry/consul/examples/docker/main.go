// Run this example after starting the adjacent compose.yaml.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	backend "github.com/apus-run/gala/plugins/registry/consul"
	"github.com/apus-run/gala/registry"
)

func main() {
	address := flag.String("address", "http://127.0.0.1:8500", "backend HTTP endpoint")
	timeout := flag.Duration("timeout", 30*time.Second, "overall example timeout")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	err := run(ctx, *address)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, address string) (err error) {
	r, err := backend.NewRegistry(backend.WithAddress(address))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	d, err := backend.NewDiscovery(backend.WithAddress(address))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()

	instance := &registry.ServiceInstance{
		Name:      "gala-example-" + registry.NewID(),
		Version:   "v1",
		Endpoints: []string{"http://127.0.0.1:8080"},
		Metadata:  map[string]string{"source": "docker-example"},
	}
	// This endpoint is sample metadata; the example does not start an HTTP server.
	if err := r.Register(ctx, instance); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	fmt.Printf("registered name=%s id=%s\n", instance.Name, instance.ID)

	events, err := d.Subscribe(ctx, instance.Name)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	if err := expect(ctx, events, registry.EventTypeAdd, "v1"); err != nil {
		return err
	}

	instances, err := d.GetService(ctx, instance.Name)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	if len(instances) != 1 || !instances[0].Equal(instance) {
		return fmt.Errorf("unexpected snapshot: %v", instances)
	}
	fmt.Println("query: 1 matching instance")

	instance.Version = "v2"
	if err := r.Register(ctx, instance); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := expect(ctx, events, registry.EventTypeAdd, "v2"); err != nil {
		return err
	}

	if err := r.Deregister(ctx, instance); err != nil {
		return fmt.Errorf("deregister: %w", err)
	}
	if err := expect(ctx, events, registry.EventTypeDelete, "v2"); err != nil {
		return err
	}
	fmt.Println("OK: register, replay, query, update, deregister")
	return nil
}

func expect(ctx context.Context, events <-chan registry.Event, kind registry.EventType, version string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting for %s: %w", kind, ctx.Err())
	case event, ok := <-events:
		if !ok {
			return fmt.Errorf("subscription closed before %s", kind)
		}
		if event.Type != kind || event.Instance == nil || event.Instance.Version != version {
			return fmt.Errorf("unexpected event: %+v", event)
		}
		fmt.Printf("event: %s version=%s\n", event.Type, event.Instance.Version)
		return nil
	}
}
