package eventbus

import (
	"context"
	"sync"
)

// HistoryRecorder retains the most recent events for in-process diagnostics.
// Construct it with NewHistoryRecorder. Entries follow serialized recording order,
// not by Event.Timestamp or handler completion. It is safe for concurrent use.
// Capacity bounds the number of entries, not the size of their Data payloads.
type HistoryRecorder struct {
	mu     sync.Mutex
	events []*Event
	next   int
	count  int
}

// NewHistoryRecorder creates a fixed-capacity history. It panics if capacity
// is not positive. Recording a full history replaces its oldest entry.
func NewHistoryRecorder(capacity int) *HistoryRecorder {
	if capacity <= 0 {
		panic("eventbus: history capacity must be positive")
	}
	return &HistoryRecorder{events: make([]*Event, capacity)}
}

// Record stores an event snapshot, ignoring nil events. Event fields and
// Metadata are copied; Data remains shared and must be treated as immutable.
// The caller must not mutate the event concurrently with this call.
func (h *HistoryRecorder) Record(event *Event) {
	if event == nil {
		return
	}
	saved := event.Clone()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events[h.next] = saved
	h.next = (h.next + 1) % len(h.events)
	h.count = min(h.count+1, len(h.events))
}

// Snapshot returns entries from oldest to newest without consuming them.
// Each entry and its Metadata are copied again, so callers may change those
// fields without changing history. Data remains shared and immutable.
func (h *HistoryRecorder) Snapshot() []*Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*Event, h.count)
	for i := range out {
		index := (h.next - h.count + len(h.events) + i) % len(h.events)
		out[i] = h.events[index].Clone()
	}
	return out
}

// WrapPublisher records each non-nil publish attempt before forwarding it to
// next, including attempts that return errors. History is not a delivery receipt.
// All three publish modes retain their original context, event, and error behavior.
// The original bus still owns subscriptions and Close. Calls bypassing this
// wrapper are not recorded. It panics if next is nil.
// Wrapping this recorder's immediate wrapper again returns it unchanged.
func (h *HistoryRecorder) WrapPublisher(next Publisher) Publisher {
	if next == nil {
		panic("eventbus: history publisher must not be nil")
	}
	if wrapped, ok := next.(*historyPublisher); ok && wrapped.history == h {
		return next
	}
	return &historyPublisher{next: next, history: h}
}

type historyPublisher struct {
	next    Publisher
	history *HistoryRecorder
}

var _ Publisher = (*historyPublisher)(nil)

func (p *historyPublisher) Publish(ctx context.Context, event *Event) error {
	p.history.Record(event)
	return p.next.Publish(ctx, event)
}

func (p *historyPublisher) PublishAsync(ctx context.Context, event *Event) error {
	p.history.Record(event)
	return p.next.PublishAsync(ctx, event)
}

func (p *historyPublisher) PublishAndWait(ctx context.Context, event *Event) error {
	p.history.Record(event)
	return p.next.PublishAndWait(ctx, event)
}
