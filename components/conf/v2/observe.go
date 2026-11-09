package conf

import (
	"context"
	"reflect"
)

// Event contains read-only snapshots. Err reports a monitoring/reload failure;
// Current then remains the last valid snapshot. Slow subscribers receive the
// latest queued event, not an unbounded history of every intermediate version.
type Event[T any] struct {
	Previous, Current *T
	Err               error
}

// Subscribe registers a bounded notification stream. Cancel releases it;
// Config.Close also closes it. Registration itself starts no goroutine.
func (c *Config[T]) Subscribe() (<-chan Event[T], func()) {
	events, cancel, _ := c.subscribe()
	return events, cancel
}
func (c *Config[T]) subscribe() (<-chan Event[T], func(), *T) {
	ch := make(chan Event[T], 1)
	c.subMu.Lock()
	if c.closed.Load() || c.subscribers == nil {
		close(ch)
	} else {
		c.subscribers[ch] = struct{}{}
	}
	initial := c.Current()
	c.subMu.Unlock()
	cancel := func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		if _, ok := c.subscribers[ch]; ok {
			delete(c.subscribers, ch)
			close(ch)
		}
	}
	return ch, cancel, initial
}
func (c *Config[T]) broadcast(event Event[T]) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if event.Err != nil {
		event.Current = c.Current()
	}
	for ch := range c.subscribers {
		select {
		case ch <- event:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- event
		}
	}
}

// Change is a field-level change selected from complete published snapshots.
type Change[V any] struct{ Previous, Current V }

// Observe provides a type-safe alternative to string-key observers. selectValue
// must only read its snapshot, must not retain mutable state, and must return
// promptly. Cancellation waits for the observer goroutine to finish.
func Observe[T, V any](ctx context.Context, c *Config[T], selectValue func(*T) V) (<-chan Change[V], func()) {
	out := make(chan Change[V], 1)
	events, unsubscribe, initial := c.subscribe()
	observeCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(out)
		defer unsubscribe()
		defer cancel()
		if initial == nil || selectValue == nil {
			return
		}
		previous := selectValue(initial)
		for {
			select {
			case <-observeCtx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Current == nil {
					continue
				}
				current := selectValue(event.Current)
				if reflect.DeepEqual(previous, current) {
					continue
				}
				change := Change[V]{Previous: previous, Current: current}
				previous = current
				select {
				case out <- change:
				default:
					select {
					case <-out:
					default:
					}
					out <- change
				}
			}
		}
	}()
	return out, func() { cancel(); <-done }
}
