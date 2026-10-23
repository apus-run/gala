package hook

import "testing"

type benchEvent struct {
	Event
	tags []string
}

func (e *benchEvent) Tags() []string { return append([]string(nil), e.tags...) } // allocates, like most real Tags()

func benchHook(n int) *Hook[*benchEvent] {
	h := &Hook[*benchEvent]{}
	for i := 0; i < n; i++ {
		h.BindFunc(func(e *benchEvent) error { return e.Next() })
	}
	return h
}

func BenchmarkTrigger10(b *testing.B) {
	h := benchHook(10)
	e := &benchEvent{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Trigger(e)
	}
}

func BenchmarkTriggerParallel10(b *testing.B) {
	h := benchHook(10)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		e := &benchEvent{}
		for pb.Next() {
			_ = h.Trigger(e)
		}
	})
}

func BenchmarkTaggedTrigger10NoTags(b *testing.B) {
	h := &Hook[*benchEvent]{}
	th := NewTaggedHook(h)
	for i := 0; i < 10; i++ {
		th.BindFunc(func(e *benchEvent) error { return e.Next() })
	}
	e := &benchEvent{tags: []string{"a", "b"}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Trigger(e)
	}
}
