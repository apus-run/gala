package plugin

import (
	"reflect"
	"testing"
)

func resetRegistry() {
	mu.Lock()
	registry = make(map[string]Plugin)
	mu.Unlock()
}

func TestRegister_nilIgnored(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	Register(nil)

	if len(All()) != 0 {
		t.Fatalf("Register(nil) should not register anything, got %d plugins", len(All()))
	}
}

func TestRegister_andGet(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	p := stubPlugin{name: "alpha"}
	Register(p)

	got, ok := Get("alpha")
	if !ok {
		t.Fatal("Get(alpha): expected ok true")
	}
	if got.Name() != "alpha" {
		t.Fatalf("Get(alpha).Name() = %q, want alpha", got.Name())
	}

	if _, ok := Get("missing"); ok {
		t.Fatal("Get(missing): expected ok false")
	}
}

func TestRegister_duplicateNameOverwrites(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	Register(stubPlugin{name: "x", tag: "first"})
	Register(stubPlugin{name: "x", tag: "second"})

	got, ok := Get("x")
	if !ok {
		t.Fatal("Get(x): expected ok true")
	}
	if sp, ok := got.(stubPlugin); !ok || sp.tag != "second" {
		t.Fatalf("expected second registration to win, got %#v", got)
	}
}

func TestAll_sortedByName(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	Register(stubPlugin{name: "gamma"})
	Register(stubPlugin{name: "alpha"})
	Register(stubPlugin{name: "beta"})

	all := All()
	names := make([]string, len(all))
	for i, p := range all {
		names[i] = p.Name()
	}
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("All() order = %v, want %v", names, want)
	}
}

func TestAll_empty(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	all := All()
	if all == nil {
		t.Fatal("All() with empty registry should return non-nil empty slice")
	}
	if len(all) != 0 {
		t.Fatalf("len(All()) = %d, want 0", len(all))
	}
}

type stubPlugin struct {
	name string
	tag  string
}

func (s stubPlugin) Name() string { return s.name }

func (stubPlugin) Register(any) {}
