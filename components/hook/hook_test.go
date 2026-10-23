package hook

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type testEvent struct {
	Event

	tags     []string
	log      []string
	tagCalls int
}

func (e *testEvent) Tags() []string {
	e.tagCalls++
	return e.tags
}

// step returns a handler that records its name before and after the rest of the chain.
func step(name string) func(*testEvent) error {
	return func(e *testEvent) error {
		e.log = append(e.log, name+">")
		err := e.Next()
		e.log = append(e.log, "<"+name)
		return err
	}
}

func flat(e *testEvent) string { return strings.Join(e.log, " ") }

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s: expected panic", name)
		}
	}()
	f()
}

func TestHookZeroValue(t *testing.T) {
	t.Parallel()

	var h Hook[*testEvent]
	if h.Length() != 0 {
		t.Fatal("expected 0 length")
	}
	if err := h.Trigger(&testEvent{}); err != nil {
		t.Fatal(err)
	}

	var called bool
	if err := h.Trigger(&testEvent{}, func(e *testEvent) error { called = true; return e.Next() }); err != nil || !called {
		t.Fatalf("one-off handler: err=%v called=%v", err, called)
	}
}

func TestHookOrderAndPriority(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	h.Bind(&Handler[*testEvent]{Id: "a", Func: step("a")})
	h.Bind(&Handler[*testEvent]{Id: "late", Priority: 10, Func: step("late")})
	h.Bind(&Handler[*testEvent]{Id: "b", Func: step("b")})
	h.Bind(&Handler[*testEvent]{Id: "early", Priority: -5, Func: step("early")})
	h.Bind(&Handler[*testEvent]{Id: "c", Func: step("c")})

	e := &testEvent{}
	if err := h.Trigger(e, step("oneoff")); err != nil {
		t.Fatal(err)
	}

	want := "early> a> b> c> late> oneoff> <oneoff <late <c <b <a <early"
	if got := flat(e); got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}
}

func TestHookBindReplace(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	h.Bind(&Handler[*testEvent]{Id: "a", Func: step("a")})
	h.Bind(&Handler[*testEvent]{Id: "b", Func: step("b")})
	h.Bind(&Handler[*testEvent]{Id: "c", Func: step("c")})

	// same position
	h.Bind(&Handler[*testEvent]{Id: "b", Func: step("B")})
	if h.Length() != 3 {
		t.Fatalf("expected 3 handlers, got %d", h.Length())
	}

	e := &testEvent{}
	_ = h.Trigger(e)
	if got, want := flat(e), "a> B> c> <c <B <a"; got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}

	// replace with a different priority moves it
	h.Bind(&Handler[*testEvent]{Id: "b", Priority: -1, Func: step("B2")})
	e = &testEvent{}
	_ = h.Trigger(e)
	if got, want := flat(e), "B2> a> c> <c <a <B2"; got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}
}

func TestHookBindIds(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}

	handler := &Handler[*testEvent]{Func: step("x")}
	id := h.Bind(handler)
	if id == "" || handler.Id != id {
		t.Fatalf("expected the generated id to be returned and set on the handler, got %q / %q", id, handler.Id)
	}

	// auto ids must never collide with explicit ones, whatever they are
	seen := map[string]struct{}{id: {}}
	for i := 0; i < 50; i++ {
		h.Bind(&Handler[*testEvent]{Id: fmt.Sprintf("h%s", strings36(i)), Func: step("explicit")})
	}
	for i := 0; i < 200; i++ {
		id := h.BindFunc(step("auto"))
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicated id %q", id)
		}
		seen[id] = struct{}{}
	}

	if got, want := h.Length(), 1+50+200; got != want {
		// a collision would have replaced an existing handler
		t.Fatalf("expected %d handlers, got %d", want, got)
	}
}

func strings36(i int) string { return fmt.Sprintf("%x", i) }

func TestHookBindCopiesHandler(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}

	var calls atomic.Int32
	handler := &Handler[*testEvent]{Id: "a", Func: func(e *testEvent) error { calls.Add(1); return e.Next() }}
	h.Bind(handler)
	h.Bind(&Handler[*testEvent]{Id: "z", Priority: 1, Func: step("z")})

	// mutating the original after Bind must not change (or race with) the hook
	handler.Func = func(e *testEvent) error { t.Error("must not be called"); return nil }
	handler.Priority = 100

	_ = h.Trigger(&testEvent{})
	if calls.Load() != 1 {
		t.Fatalf("expected the originally bound func to run once, got %d", calls.Load())
	}
}

func TestHookBindNil(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	mustPanic(t, "nil handler", func() { h.Bind(nil) })
	mustPanic(t, "nil func", func() { h.Bind(&Handler[*testEvent]{}) })
	mustPanic(t, "BindFunc nil", func() { h.BindFunc(nil) })

	if h.Length() != 0 {
		t.Fatal("nothing should have been registered")
	}
}

func TestHookUnbind(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	h.Bind(&Handler[*testEvent]{Id: "a", Func: step("a")})
	h.Bind(&Handler[*testEvent]{Id: "b", Func: step("b")})
	h.Bind(&Handler[*testEvent]{Id: "c", Func: step("c")})
	h.Bind(&Handler[*testEvent]{Id: "d", Func: step("d")})

	h.Unbind()
	h.Unbind("missing")
	if h.Length() != 4 {
		t.Fatalf("expected 4, got %d", h.Length())
	}

	h.Unbind("b", "d", "missing")
	if h.Length() != 2 {
		t.Fatalf("expected 2, got %d", h.Length())
	}

	e := &testEvent{}
	_ = h.Trigger(e)
	if got, want := flat(e), "a> c> <c <a"; got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}

	h.UnbindAll()
	if h.Length() != 0 {
		t.Fatalf("expected 0, got %d", h.Length())
	}
	e = &testEvent{}
	_ = h.Trigger(e)
	if len(e.log) != 0 {
		t.Fatalf("unexpected calls: %v", e.log)
	}
}

func TestHookChainControl(t *testing.T) {
	t.Parallel()

	errStop := errors.New("stop")

	t.Run("not calling Next stops the chain", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(step("a"))
		h.BindFunc(func(e *testEvent) error { e.log = append(e.log, "stopper"); return nil })
		h.BindFunc(step("c"))

		e := &testEvent{}
		if err := h.Trigger(e); err != nil {
			t.Fatal(err)
		}
		if got, want := flat(e), "a> stopper <a"; got != want {
			t.Fatalf("\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("errors bubble up", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(step("a"))
		h.BindFunc(func(e *testEvent) error { return errStop })
		h.BindFunc(step("c"))

		e := &testEvent{}
		if err := h.Trigger(e); !errors.Is(err, errStop) {
			t.Fatalf("expected errStop, got %v", err)
		}
		if got, want := flat(e), "a> <a"; got != want {
			t.Fatalf("\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("one-off error", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(step("a"))

		err := h.Trigger(&testEvent{}, func(e *testEvent) error { return errStop })
		if !errors.Is(err, errStop) {
			t.Fatalf("expected errStop, got %v", err)
		}
	})

	t.Run("a handler can swallow a downstream error", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(func(e *testEvent) error {
			if err := e.Next(); err != nil {
				e.log = append(e.log, "recovered")
			}
			return nil
		})
		h.BindFunc(func(e *testEvent) error { return errStop })

		e := &testEvent{}
		if err := h.Trigger(e); err != nil || flat(e) != "recovered" {
			t.Fatalf("err=%v log=%v", err, e.log)
		}
	})

	t.Run("calling Next twice doesn't re-run the finished chain", func(t *testing.T) {
		var downstream int

		h := &Hook[*testEvent]{}
		h.BindFunc(func(e *testEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			return e.Next()
		})
		h.BindFunc(func(e *testEvent) error { downstream++; return e.Next() })

		if err := h.Trigger(&testEvent{}); err != nil {
			t.Fatal(err)
		}
		if downstream != 1 {
			t.Fatalf("expected 1 downstream run, got %d", downstream)
		}
	})
}

func TestHookEventReuseAndReentrancy(t *testing.T) {
	t.Parallel()

	t.Run("sequential reuse", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(step("a"))
		h.BindFunc(func(e *testEvent) error { e.log = append(e.log, "stop"); return nil }) // leaves the chain unfinished

		e := &testEvent{}
		_ = h.Trigger(e)
		_ = h.Trigger(e)

		if got, want := flat(e), "a> stop <a a> stop <a"; got != want {
			t.Fatalf("\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("the event is clean after Trigger", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(func(e *testEvent) error { e.log = append(e.log, "stop"); return nil }) // never calls Next
		h.BindFunc(step("never"))

		e := &testEvent{}
		_ = h.Trigger(e)

		if err := e.Next(); err != nil || flat(e) != "stop" {
			t.Fatalf("a late Next() must do nothing, err=%v log=%v", err, e.log)
		}
		if e.nextFunc() != nil {
			t.Fatal("expected the event to be reset")
		}
	})

	t.Run("nested Trigger of the same event keeps the outer chain intact", func(t *testing.T) {
		inner := &Hook[*testEvent]{}
		inner.BindFunc(step("i1"))
		inner.BindFunc(step("i2"))

		outer := &Hook[*testEvent]{}
		outer.BindFunc(step("o1"))
		outer.BindFunc(func(e *testEvent) error {
			e.log = append(e.log, "o2>")
			if err := inner.Trigger(e); err != nil {
				return err
			}
			err := e.Next() // must continue with o3, not with the inner chain
			e.log = append(e.log, "<o2")
			return err
		})
		outer.BindFunc(step("o3"))

		e := &testEvent{}
		if err := outer.Trigger(e); err != nil {
			t.Fatal(err)
		}

		want := "o1> o2> i1> i2> <i2 <i1 o3> <o3 <o2 <o1"
		if got := flat(e); got != want {
			t.Fatalf("\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("recursive Trigger of the same hook", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		var depth int
		h.BindFunc(func(e *testEvent) error {
			depth++
			if depth < 3 {
				if err := h.Trigger(e); err != nil {
					return err
				}
			}
			e.log = append(e.log, fmt.Sprint("d", depth))
			return e.Next()
		})

		e := &testEvent{}
		if err := h.Trigger(e); err != nil {
			t.Fatal(err)
		}
		if len(e.log) != 3 {
			t.Fatalf("unexpected log %v", e.log)
		}
	})

	t.Run("event is restored even if a handler panics", func(t *testing.T) {
		h := &Hook[*testEvent]{}
		h.BindFunc(func(e *testEvent) error { panic("boom") })

		e := &testEvent{}
		func() {
			defer func() { _ = recover() }()
			_ = h.Trigger(e)
		}()

		if e.nextFunc() != nil {
			t.Fatal("expected the event to be reset after a panic")
		}
	})
}

func TestHookSnapshotIsolation(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	h.Bind(&Handler[*testEvent]{Id: "a", Func: func(e *testEvent) error {
		// changing the hook while the chain is running only affects later triggers
		h.Unbind("b")
		h.BindFunc(step("new"))
		e.log = append(e.log, "a")
		return e.Next()
	}})
	h.Bind(&Handler[*testEvent]{Id: "b", Func: step("b")})

	e := &testEvent{}
	_ = h.Trigger(e)
	if got, want := flat(e), "a b> <b"; got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}
	if h.Length() != 2 {
		t.Fatalf("expected 2 handlers (a, new), got %d", h.Length())
	}
}

func TestHookOneOffDoesNotLeak(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	h.BindFunc(step("a"))

	var runs int
	oneOff := func(e *testEvent) error { runs++; return e.Next() }

	_ = h.Trigger(&testEvent{}, oneOff)
	_ = h.Trigger(&testEvent{})

	if runs != 1 || h.Length() != 1 {
		t.Fatalf("runs=%d length=%d", runs, h.Length())
	}
}

// Meant to be run with -race.
func TestHookConcurrentUse(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	h.BindFunc(step("base"))

	var wg sync.WaitGroup
	var triggers atomic.Int64

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				e := &testEvent{} // an event per goroutine
				if err := h.Trigger(e, step("oneoff")); err != nil {
					t.Error(err)
					return
				}
				triggers.Add(1)
				h.Length()
			}
		}()
	}

	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				id := h.Bind(&Handler[*testEvent]{Id: fmt.Sprint("g", g), Priority: i % 3, Func: step("dyn")})
				if i%5 == 0 {
					h.Unbind(id)
				}
				h.BindFunc(step("auto"))
				if i%50 == 0 {
					h.UnbindAll()
					h.BindFunc(step("base"))
				}
			}
		}(g)
	}

	wg.Wait()

	if triggers.Load() != 4*500 {
		t.Fatalf("unexpected triggers count %d", triggers.Load())
	}
}

func TestEventZeroValue(t *testing.T) {
	t.Parallel()

	var e Event
	if err := e.Next(); err != nil {
		t.Fatal(err)
	}

	var r Resolver = &e
	if !reflect.DeepEqual(r.nextFunc(), (func() error)(nil)) {
		t.Fatal("expected nil next func")
	}
}
