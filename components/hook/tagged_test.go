package hook

import (
	"sync"
	"testing"
)

func TestTaggedHookCanTriggerOn(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		hookTags []string
		check    []string
		want     bool
	}{
		{nil, nil, true},
		{nil, []string{"a"}, true},
		{[]string{"a", "b"}, nil, false},
		{[]string{"a", "b"}, []string{"c"}, false},
		{[]string{"a", "b"}, []string{"c", "b"}, true},
	}

	for i, s := range scenarios {
		th := NewTaggedHook(&Hook[*testEvent]{}, s.hookTags...)
		if got := th.CanTriggerOn(s.check); got != s.want {
			t.Errorf("%d: want %v, got %v", i, s.want, got)
		}
	}
}

func TestTaggedHookTrigger(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}

	h.BindFunc(step("main"))
	NewTaggedHook(h, "posts").BindFunc(step("posts"))
	NewTaggedHook(h, "users", "admins").BindFunc(step("users"))
	NewTaggedHook(h).BindFunc(step("all"))
	h.BindFunc(step("main2"))

	scenarios := []struct {
		tags []string
		want string
	}{
		{nil, "main> all> main2> <main2 <all <main"},
		{[]string{"posts"}, "main> posts> all> main2> <main2 <all <posts <main"},
		{[]string{"admins", "x"}, "main> users> all> main2> <main2 <all <users <main"},
		{[]string{"posts", "users"}, "main> posts> users> all> main2> <main2 <all <users <posts <main"},
	}

	for _, s := range scenarios {
		e := &testEvent{tags: s.tags}
		if err := h.Trigger(e); err != nil {
			t.Fatal(err)
		}
		if got := flat(e); got != s.want {
			t.Errorf("tags %v:\nwant %s\ngot  %s", s.tags, s.want, got)
		}
	}
}

func TestTaggedHookBind(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	posts := NewTaggedHook(h, "posts")
	users := NewTaggedHook(h, "users")

	called := 0
	handler := &Handler[*testEvent]{
		Func:     func(e *testEvent) error { called++; return e.Next() },
		Priority: -1,
	}

	id := posts.Bind(handler)
	if id == "" || handler.Id != id {
		t.Fatalf("expected the id to be set on the handler, got %q / %q", id, handler.Id)
	}

	// the same handler can be bound again (to another tagged hook) without being wrapped twice:
	// it replaces the previous one because the id is the same
	handler.Id = ""
	users.Bind(handler)
	if h.Length() != 2 {
		t.Fatalf("expected 2 handlers, got %d", h.Length())
	}

	_ = h.Trigger(&testEvent{tags: []string{"posts"}})
	if called != 1 {
		t.Fatalf("expected a single call, got %d", called)
	}

	// replace by id through the tagged hook
	var replaced int
	users.Bind(&Handler[*testEvent]{Id: id, Func: func(e *testEvent) error { replaced++; return e.Next() }})
	_ = h.Trigger(&testEvent{tags: []string{"posts"}})
	_ = h.Trigger(&testEvent{tags: []string{"users"}})
	if replaced != 1 {
		t.Fatalf("expected the replacement to run once for the users tag, got %d", replaced)
	}

	// unbinding works via the embedded hook API
	posts.Unbind(id)
}

func TestTaggedHookBindNil(t *testing.T) {
	t.Parallel()

	th := NewTaggedHook(&Hook[*testEvent]{}, "a")
	mustPanic(t, "nil handler", func() { th.Bind(nil) })
	mustPanic(t, "nil func", func() { th.Bind(&Handler[*testEvent]{}) })
	mustPanic(t, "BindFunc nil", func() { th.BindFunc(nil) })
	if th.Length() != 0 {
		t.Fatal("nothing should have been registered")
	}
}

func TestTaggedHookCopiesTags(t *testing.T) {
	t.Parallel()

	tags := []string{"a"}
	h := &Hook[*testEvent]{}
	NewTaggedHook(h, tags...).BindFunc(step("a"))
	tags[0] = "changed"

	e := &testEvent{tags: []string{"a"}}
	_ = h.Trigger(e)
	if got, want := flat(e), "a> <a"; got != want {
		t.Fatalf("\nwant %s\ngot  %s", want, got)
	}
}

func TestTaggedHookSkipsTagsLookupWhenUntagged(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	NewTaggedHook(h).BindFunc(step("all"))
	NewTaggedHook(h, "x").BindFunc(step("x"))

	e := &testEvent{tags: []string{"x"}}
	_ = h.Trigger(e)

	// only the tagged handler needs the event tags
	if e.tagCalls != 1 {
		t.Fatalf("expected Tags() to be called once, got %d", e.tagCalls)
	}
}

// Meant to be run with -race.
func TestTaggedHookConcurrentUse(t *testing.T) {
	t.Parallel()

	h := &Hook[*testEvent]{}
	th := NewTaggedHook(h, "a")

	var wg sync.WaitGroup
	for g := 0; g < 3; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				_ = h.Trigger(&testEvent{tags: []string{"a"}})
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				th.Unbind(th.BindFunc(step("a")))
				th.BindFunc(step("a"))
			}
		}()
	}
	wg.Wait()
}
