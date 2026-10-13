package eventbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestHistoryRecorderRejectsNonpositiveCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("nonpositive capacity did not panic")
				}
			}()
			NewHistoryRecorder(capacity)
		})
	}
}

func TestHistoryRecorderRetainsLatest(t *testing.T) {
	for _, capacity := range []int{1, 3} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			history := NewHistoryRecorder(capacity)
			history.Record(nil)
			if len(history.Snapshot()) != 0 {
				t.Fatal("empty history or nil record produced entries")
			}
			for i := range 8 {
				history.Record(&Event{ID: fmt.Sprint(i)})
				entries := history.Snapshot()
				count := min(i+1, capacity)
				if len(entries) != count {
					t.Fatalf("after record %d: entries=%d, want %d", i, len(entries), count)
				}
				for j, entry := range entries {
					if want := fmt.Sprint(i - count + 1 + j); entry.ID != want {
						t.Errorf("entry[%d]=%q, want %q", j, entry.ID, want)
					}
				}
			}
			if len(history.Snapshot()) != capacity {
				t.Fatal("Snapshot consumed entries")
			}
		})
	}
}

func TestHistoryRecorderSnapshotOwnership(t *testing.T) {
	payload := &struct{ Value string }{"shared"}
	history := NewHistoryRecorder(2)
	event := &Event{ID: "original", Type: "created", Data: payload, Metadata: map[string]string{"owner": "producer"}}
	history.Record(event)
	event.ID = "changed"
	event.Metadata["owner"] = "changed"
	first := history.Snapshot()
	if first[0].ID != "original" || first[0].Metadata["owner"] != "producer" {
		t.Fatalf("producer mutation changed history: %#v", first[0])
	}
	if first[0].Data != payload {
		t.Fatal("Data no longer follows the shared, immutable payload contract")
	}
	first[0].ID = "snapshot-only"
	first[0].Metadata["owner"] = "snapshot-only"
	second := history.Snapshot()
	if second[0].ID != "original" || second[0].Metadata["owner"] != "producer" {
		t.Fatalf("snapshot mutation changed history: %#v", second[0])
	}
	history.Record(&Event{ID: "next"})
	history.Record(&Event{ID: "latest"})
	if second[0].ID != "original" || second[0].Metadata["owner"] != "producer" {
		t.Fatal("overwriting history changed an older snapshot")
	}
}

func TestHistoryRecorderConcurrentRecordAndSnapshot(t *testing.T) {
	const capacity, writers, records = 32, 8, 100
	history := NewHistoryRecorder(capacity)
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Go(func() {
			for sequence := range records {
				id := fmt.Sprintf("%d:%d", writer, sequence)
				history.Record(&Event{ID: id, Metadata: map[string]string{"id": id}})
			}
		})
	}
	for range 3 {
		wg.Go(func() {
			for range records {
				entries := history.Snapshot()
				if len(entries) > capacity {
					t.Error("snapshot exceeded capacity")
				}
				for _, entry := range entries {
					if entry == nil || entry.Metadata["id"] != entry.ID {
						t.Errorf("inconsistent snapshot entry: %#v", entry)
						continue
					}
					entry.Metadata["id"] = "snapshot-only"
				}
			}
		})
	}
	wg.Wait()
	entries := history.Snapshot()
	if len(entries) != capacity {
		t.Fatalf("final entries=%d, want %d", len(entries), capacity)
	}
	seen := make(map[string]bool, capacity)
	for _, entry := range entries {
		if seen[entry.ID] || entry.Metadata["id"] != entry.ID {
			t.Fatalf("duplicate or corrupted retained event: %#v", entry)
		}
		seen[entry.ID] = true
	}
}

type historyPublisherProbe struct {
	call func(string, context.Context, *Event) error
}

func (p *historyPublisherProbe) Publish(ctx context.Context, event *Event) error {
	return p.call("sync", ctx, event)
}

func (p *historyPublisherProbe) PublishAsync(ctx context.Context, event *Event) error {
	return p.call("async", ctx, event)
}

func (p *historyPublisherProbe) PublishAndWait(ctx context.Context, event *Event) error {
	return p.call("wait", ctx, event)
}

func TestHistoryPublisherModes(t *testing.T) {
	for _, mode := range []string{"sync", "async", "wait"} {
		t.Run(mode, func(t *testing.T) {
			history := NewHistoryRecorder(2)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			event := &Event{ID: "attempt", Metadata: map[string]string{"state": "before"}}
			failure := errors.New("publish failed")
			called := false
			probe := &historyPublisherProbe{call: func(actual string, gotCtx context.Context, gotEvent *Event) error {
				called = true
				if actual != mode || gotCtx != ctx || gotEvent != event {
					t.Error("wrapper changed the publish mode, context, or event")
				}
				entries := history.Snapshot()
				if len(entries) != 1 || entries[0].ID != "attempt" {
					t.Fatal("publish attempt was not recorded before forwarding")
				}
				gotEvent.ID = "delegate-only"
				gotEvent.Metadata["state"] = "delegate-only"
				return failure
			}}
			publisher := history.WrapPublisher(probe)
			methods := map[string]func(context.Context, *Event) error{
				"sync": publisher.Publish, "async": publisher.PublishAsync, "wait": publisher.PublishAndWait,
			}
			if err := methods[mode](ctx, event); err != failure || !called {
				t.Fatalf("forwarding changed error or skipped publisher: err=%v called=%v", err, called)
			}
			entries := history.Snapshot()
			if len(entries) != 1 || entries[0].ID != "attempt" || entries[0].Metadata["state"] != "before" {
				t.Fatalf("failed attempt was dropped, duplicated, or mutated: %#v", entries)
			}
		})
	}
}

func TestHistoryPublisherRecordsOnceAcrossFanOut(t *testing.T) {
	history := NewHistoryRecorder(8)
	bus := NewEventBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = bus.Close() })
	var calls atomic.Int64
	for range 2 {
		if err := bus.Subscribe("test", EventHandlerFunc(func(context.Context, *Event) error {
			calls.Add(1)
			return nil
		})); err != nil {
			t.Fatal(err)
		}
	}
	publisher := history.WrapPublisher(bus)
	for _, publish := range []func(context.Context, *Event) error{publisher.Publish, publisher.PublishAsync, publisher.PublishAndWait} {
		if err := publish(t.Context(), NewEvent("test", nil)); err != nil {
			t.Fatal(err)
		}
	}
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 6 || len(history.Snapshot()) != 3 {
		t.Fatalf("handler fan-out changed recording count: handlers=%d entries=%d", calls.Load(), len(history.Snapshot()))
	}
	if err := publisher.Publish(t.Context(), NewEvent("test", nil)); err == nil {
		t.Fatal("wrapper hid a closed bus error")
	}
	if len(history.Snapshot()) != 4 {
		t.Fatal("closed bus attempt was not recorded")
	}
}

func TestHistoryPublisherNilEventAndWrapping(t *testing.T) {
	history := NewHistoryRecorder(4)
	calls := 0
	failure := errors.New("invalid event")
	probe := &historyPublisherProbe{call: func(_ string, _ context.Context, event *Event) error {
		calls++
		if event == nil {
			return failure
		}
		return nil
	}}
	publisher := history.WrapPublisher(history.WrapPublisher(probe))
	if err := publisher.Publish(t.Context(), nil); err != failure || calls != 1 || len(history.Snapshot()) != 0 {
		t.Fatalf("nil forwarding changed: err=%v calls=%d entries=%d", err, calls, len(history.Snapshot()))
	}
	event := &Event{ID: "same"}
	for range 2 {
		if err := publisher.Publish(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	if len(history.Snapshot()) != 2 || calls != 3 {
		t.Fatal("wrapping duplicated records or repeated event IDs were deduplicated")
	}
	other := NewHistoryRecorder(4)
	if err := other.WrapPublisher(publisher).Publish(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if len(other.Snapshot()) != 1 || len(history.Snapshot()) != 3 {
		t.Fatal("independent recorders interfered with each other")
	}
}

func TestHistoryPublisherRejectsNilPublisher(t *testing.T) {
	defer func() {
		if value := recover(); value == nil || !strings.Contains(fmt.Sprint(value), "publisher") {
			t.Fatal("nil publisher was not rejected")
		}
	}()
	NewHistoryRecorder(1).WrapPublisher(nil)
}
