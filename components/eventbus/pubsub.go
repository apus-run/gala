package eventbus

import (
	"context"
	"io"
)

// PubSub combines event publishing and subscription operations.
type PubSub interface {
	Publisher
	Subscriber
	io.Closer
}

type Publisher interface {
	// Publish executes all subscribed handlers sequentially and aggregates errors.
	Publish(ctx context.Context, event *Event) error

	// PublishAsync dispatches handlers with bounded concurrency and returns after
	// every handler has been scheduled. Handler failures are logged.
	PublishAsync(ctx context.Context, event *Event) error

	// PublishAndWait executes handlers with bounded concurrency, waits for all of
	// them, and aggregates errors.
	PublishAndWait(ctx context.Context, event *Event) error
}

type Subscriber interface {
	// Subscribe registers a handler for a specific event type
	Subscribe(eventType EventType, handler Handler) error

	// SubscribeWithCancel registers a handler and returns an identity-safe cancel function.
	SubscribeWithCancel(eventType EventType, handler Handler) (cancel func() error, err error)

	// SubscribeOnce registers a handler that will be called only once
	SubscribeOnce(eventType EventType, handler Handler) error

	// SubscribeOnceWithCancel registers a one-shot handler with identity-safe cancellation.
	SubscribeOnceWithCancel(eventType EventType, handler Handler) (cancel func() error, err error)

	// Unsubscribe removes a handler whose identity is safely comparable. Function
	// handlers must use the cancel function returned by SubscribeWithCancel.
	Unsubscribe(eventType EventType, handler Handler) error
}
