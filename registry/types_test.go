package registry

import (
	"slices"
	"sync"
	"testing"
)

func newIns() *ServiceInstance {
	return &ServiceInstance{
		ID:        "i-1",
		Name:      "svc",
		Version:   "v1",
		Metadata:  map[string]string{"a": "1", MetaWeight: "10"},
		Endpoints: []string{"grpc://127.0.0.1:9000", "http://127.0.0.1:8000"},
	}
}

func TestServiceInstance_Equal(t *testing.T) {
	var nilIns *ServiceInstance

	tests := []struct {
		name string
		a, b *ServiceInstance
		mut  func(a, b *ServiceInstance)
		want bool
	}{
		{name: "both nil", a: nilIns, b: nilIns, want: true},
		{name: "nil vs non-nil", a: nilIns, b: newIns(), want: false},
		{name: "non-nil vs nil", a: newIns(), b: nilIns, want: false},
		{name: "same pointer", a: newIns(), want: true, mut: func(a, b *ServiceInstance) {}},
		{name: "identical", a: newIns(), b: newIns(), want: true},
		{
			name: "endpoint order ignored",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { slices.Reverse(b.Endpoints) },
			want: true,
		},
		{
			name: "endpoint differs",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { b.Endpoints[0] = "grpc://10.0.0.1:9000" },
			want: false,
		},
		{
			name: "endpoint count differs",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { b.Endpoints = b.Endpoints[:1] },
			want: false,
		},
		{
			name: "duplicate endpoints are not a set",
			a:    &ServiceInstance{Endpoints: []string{"x", "x", "y"}},
			b:    &ServiceInstance{Endpoints: []string{"x", "y", "y"}},
			want: false,
		},
		{
			// Regression: gala's loop-based comparison treated these as equal
			// because t.Metadata["a"] returns "" for a missing key.
			name: "metadata same length, different keys, empty values",
			a:    &ServiceInstance{Metadata: map[string]string{"a": ""}},
			b:    &ServiceInstance{Metadata: map[string]string{"b": ""}},
			want: false,
		},
		{
			name: "metadata value differs",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { b.Metadata["a"] = "2" },
			want: false,
		},
		{
			name: "nil metadata equals empty metadata",
			a:    &ServiceInstance{Name: "svc"},
			b:    &ServiceInstance{Name: "svc", Metadata: map[string]string{}},
			want: true,
		},
		{
			name: "id differs",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { b.ID = "i-2" },
			want: false,
		},
		{
			name: "name differs",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { b.Name = "other" },
			want: false,
		},
		{
			name: "version differs",
			a:    newIns(), b: newIns(),
			mut:  func(a, b *ServiceInstance) { b.Version = "v2" },
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := tt.a, tt.b
			if tt.name == "same pointer" {
				b = a
			}
			if tt.mut != nil {
				tt.mut(a, b)
			}
			if got := a.Equal(b); got != tt.want {
				t.Fatalf("Equal() = %v, want %v", got, tt.want)
			}
			// Equality must be symmetric.
			if got := b.Equal(a); got != tt.want {
				t.Fatalf("reverse Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Equal must not reorder either instance's Endpoints (gala's version sorted
// them in place).
func TestServiceInstance_Equal_DoesNotMutate(t *testing.T) {
	a := &ServiceInstance{Endpoints: []string{"z", "a", "m"}}
	b := &ServiceInstance{Endpoints: []string{"m", "z", "a"}}

	if !a.Equal(b) {
		t.Fatal("expected equal")
	}
	if want := []string{"z", "a", "m"}; !slices.Equal(a.Endpoints, want) {
		t.Fatalf("receiver Endpoints mutated: %v", a.Endpoints)
	}
	if want := []string{"m", "z", "a"}; !slices.Equal(b.Endpoints, want) {
		t.Fatalf("argument Endpoints mutated: %v", b.Endpoints)
	}
}

// Run with -race: concurrent Equal on shared instances must be safe.
func TestServiceInstance_Equal_Concurrent(t *testing.T) {
	a, b := newIns(), newIns()
	slices.Reverse(b.Endpoints)

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if !a.Equal(b) {
					t.Error("expected equal")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestServiceInstance_Clone(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var ins *ServiceInstance
		if ins.Clone() != nil {
			t.Fatal("Clone of nil should be nil")
		}
	})

	t.Run("deep copy", func(t *testing.T) {
		orig := newIns()
		c := orig.Clone()

		if c == orig {
			t.Fatal("Clone returned the same pointer")
		}
		if !orig.Equal(c) {
			t.Fatal("clone should equal the original")
		}

		c.Endpoints[0] = "changed"
		c.Metadata["a"] = "changed"
		c.Metadata["new"] = "x"
		c.Name = "changed"

		if orig.Endpoints[0] != "grpc://127.0.0.1:9000" {
			t.Fatal("Endpoints shared with the clone")
		}
		if orig.Metadata["a"] != "1" || len(orig.Metadata) != 2 {
			t.Fatal("Metadata shared with the clone")
		}
		if orig.Name != "svc" {
			t.Fatal("Name changed")
		}
	})

	t.Run("nil fields stay nil", func(t *testing.T) {
		c := (&ServiceInstance{Name: "svc"}).Clone()
		if c.Metadata != nil || c.Endpoints != nil {
			t.Fatalf("expected nil fields, got %+v", c)
		}
	})
}

func TestServiceInstance_String(t *testing.T) {
	var nilIns *ServiceInstance
	if got := nilIns.String(); got != "<nil>" {
		t.Fatalf("nil String() = %q", got)
	}
	if got := newIns().String(); got != "svc-i-1" {
		t.Fatalf("String() = %q", got)
	}
}

func TestEventType(t *testing.T) {
	tests := []struct {
		typ            EventType
		str            string
		isAdd, isDelet bool
	}{
		{EventTypeAdd, "add", true, false},
		{EventTypeDelete, "delete", false, true},
		{EventTypeUnknown, "unknown", false, false},
		{EventType(99), "unknown", false, false},
	}
	for _, tt := range tests {
		if got := tt.typ.String(); got != tt.str {
			t.Errorf("%d String() = %q, want %q", tt.typ, got, tt.str)
		}
		if tt.typ.IsAdd() != tt.isAdd {
			t.Errorf("%d IsAdd() = %v", tt.typ, tt.typ.IsAdd())
		}
		if tt.typ.IsDelete() != tt.isDelet {
			t.Errorf("%d IsDelete() = %v", tt.typ, tt.typ.IsDelete())
		}
	}
}
