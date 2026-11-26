package job

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRegisterValidation(t *testing.T) {
	r := NewRegistry()

	if err := r.Register("", func(context.Context) error { return nil }); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("empty name: got %v, want ErrEmptyName", err)
	}
	if err := r.Register("x", nil); !errors.Is(err, ErrNilFunc) {
		t.Fatalf("nil func: got %v, want ErrNilFunc", err)
	}
	if err := r.Register("x", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register("x", func(context.Context) error { return nil }); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: got %v, want ErrDuplicate", err)
	}
}

func TestRunUnknownJob(t *testing.T) {
	r := NewRegistry()

	err := r.Run(context.Background(), Invocation{Name: "missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error %q does not name the job", err)
	}
}

func TestRunSeesInvocation(t *testing.T) {
	r := NewRegistry()

	var (
		got Invocation
		ok  bool
	)
	header := http.Header{"X-Test": {"one"}}
	want := Invocation{
		Name:    "greet",
		RunID:   "run-1",
		Payload: []byte("hello"),
		Header:  header,
	}
	if err := r.Register("greet", func(ctx context.Context) error {
		got, ok = FromContext(ctx)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.Run(context.Background(), want); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !ok {
		t.Fatal("invocation not found in job context")
	}
	if got.Name != want.Name || got.RunID != want.RunID {
		t.Fatalf("got name/runID %q/%q, want %q/%q", got.Name, got.RunID, want.Name, want.RunID)
	}
	if !slices.Equal(got.Payload, want.Payload) {
		t.Fatalf("got payload %q, want %q", got.Payload, want.Payload)
	}
	if got.Header.Get("X-Test") != "one" {
		t.Fatalf("got header %v, want X-Test: one", got.Header)
	}
}

func TestFromContextWithoutInvocation(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("background context unexpectedly carries an invocation")
	}
	if _, ok := FromContext(nil); ok {
		t.Fatal("nil context unexpectedly carries an invocation")
	}

	ctx := NewContext(nil, Invocation{Name: "x"})
	if inv, ok := FromContext(ctx); !ok || inv.Name != "x" {
		t.Fatalf("NewContext(nil) lost the invocation: %+v, %v", inv, ok)
	}

	type ctxKey struct{}
	parent := context.WithValue(context.Background(), ctxKey{}, "kept")
	ctx = NewContext(parent, Invocation{Name: "y"})
	if ctx.Value(ctxKey{}) != "kept" {
		t.Fatal("NewContext dropped parent values")
	}
}

func TestRunJobError(t *testing.T) {
	r := NewRegistry()
	errJob := errors.New("job failed")
	if err := r.Register("fail", func(context.Context) error { return errJob }); err != nil {
		t.Fatalf("register: %v", err)
	}

	err := r.Run(context.Background(), Invocation{Name: "fail"})
	if !errors.Is(err, errJob) {
		t.Fatalf("got %v, want %v", err, errJob)
	}
}

func TestRunRecoversPanic(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("boom", func(context.Context) error {
		panic("kaboom")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	err := r.Run(context.Background(), Invocation{Name: "boom"})

	var pe *PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("got %T (%v), want *PanicError", err, err)
	}
	if pe.Value != "kaboom" {
		t.Fatalf("got panic value %v, want kaboom", pe.Value)
	}
	if len(pe.Stack) == 0 {
		t.Fatal("panic stack is empty")
	}
	if !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("error %q does not mention the panic value", err)
	}
	if strings.Contains(err.Error(), "goroutine") {
		t.Fatal("Error() leaks the stack; it may be written to an HTTP response")
	}
}

func TestNamesSortedSnapshot(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"b", "a", "c"} {
		if err := r.Register(name, func(context.Context) error { return nil }); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}

	got := r.Names()
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	got[0] = "mutated"
	if again := r.Names(); !slices.Equal(again, want) {
		t.Fatalf("Names is not a snapshot: %v", again)
	}
}

func TestRegistriesAreIndependent(t *testing.T) {
	r1 := NewRegistry()
	r2 := NewRegistry()

	if err := r1.Register("only-1", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("register on r1: %v", err)
	}
	if err := r2.Register("only-2", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("register on r2: %v", err)
	}

	if got := r1.Names(); !slices.Equal(got, []string{"only-1"}) {
		t.Fatalf("r1 names: %v", got)
	}
	if got := r2.Names(); !slices.Equal(got, []string{"only-2"}) {
		t.Fatalf("r2 names: %v", got)
	}
	if err := r1.Run(context.Background(), Invocation{Name: "only-2"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("r1 ran a job registered on r2: %v", err)
	}
}

func TestRegisterConcurrent(t *testing.T) {
	r := NewRegistry()

	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := r.Register(fmt.Sprintf("job-%02d", i), func(context.Context) error { return nil }); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent register: %v", err)
	}
	if got := len(r.Names()); got != n {
		t.Fatalf("got %d names, want %d", got, n)
	}
}

func TestRegisterConcurrentDuplicate(t *testing.T) {
	r := NewRegistry()

	const n = 32
	var (
		wg       sync.WaitGroup
		accepted atomic.Int32
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Register("same", func(context.Context) error { return nil })
			switch {
			case err == nil:
				accepted.Add(1)
			case !errors.Is(err, ErrDuplicate):
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted %d concurrent registrations, want exactly 1", got)
	}
}
