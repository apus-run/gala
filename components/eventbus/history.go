package eventbus

import (
	"context"
	"sync"
)

// HistoryRecorder retains the most recent events for in-process diagnostics.
// Construct it with NewHistoryRecorder. Entries follow serialized recording order,
// not by Event.Timestamp or handler completion. It is safe for concurrent use.
// Capacity bounds the number of entries, not the size of their Data payloads.
// HistoryRecorder 保留最近的 events 用于进程内诊断，用 NewHistoryRecorder 构造。
// 条目按串行记录顺序排列，而非按 Event.Timestamp 或 handler 完成顺序。并发安全。
// 容量限制的是条目数，不是各条目 Data 负载的大小。
type HistoryRecorder struct {
	mu     sync.Mutex
	events []*Event
	next   int
	count  int
}

// NewHistoryRecorder creates a fixed-capacity history. It panics if capacity
// is not positive. Recording a full history replaces its oldest entry.
// NewHistoryRecorder 创建固定容量的历史记录；容量非正时 panic。历史写满后
// 再记录会覆盖最老的条目。
func NewHistoryRecorder(capacity int) *HistoryRecorder {
	if capacity <= 0 {
		panic("eventbus: history capacity must be positive")
	}
	return &HistoryRecorder{events: make([]*Event, capacity)}
}

// Record stores an event snapshot, ignoring nil events. Event fields and
// Metadata are copied; Data remains shared and must be treated as immutable.
// The caller must not mutate the event concurrently with this call.
// Record 存入事件的快照，忽略 nil 事件。Event 字段与 Metadata 会被复制；
// Data 保持共享，必须当作不可变。调用期间调用方不得并发修改该事件。
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
// Snapshot 从最老到最新返回条目，且不消耗它们。每个条目及其 Metadata 会再次
// 复制，调用方修改这些字段不会影响历史。Data 保持共享且不可变。
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
// WrapPublisher 在把发布请求转发给 next 之前记录每次非 nil 的发布尝试（包括
// 返回错误的尝试）。历史不是送达回执。三种发布模式都保持原有的 context、
// event 与错误行为。订阅与 Close 仍归原总线所有；绕过该包装器的调用不会被
// 记录。next 为 nil 时 panic。重复包装本记录器的直接包装器会原样返回。
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

// Publish records the event before forwarding it | Publish 先记录事件再转发.
func (p *historyPublisher) Publish(ctx context.Context, event *Event) error {
	p.history.Record(event)
	return p.next.Publish(ctx, event)
}

// PublishAsync records the event before forwarding it | PublishAsync 先记录事件再转发.
func (p *historyPublisher) PublishAsync(ctx context.Context, event *Event) error {
	p.history.Record(event)
	return p.next.PublishAsync(ctx, event)
}

// PublishAndWait records the event before forwarding it | PublishAndWait 先记录事件再转发.
func (p *historyPublisher) PublishAndWait(ctx context.Context, event *Event) error {
	p.history.Record(event)
	return p.next.PublishAndWait(ctx, event)
}
