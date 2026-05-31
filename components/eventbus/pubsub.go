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
	// Publish publishes an event to all subscribed handlers
	Publish(ctx context.Context, event *Event) error
}

type Subscriber interface {
	// Subscribe registers a handler for a specific event type
	Subscribe(eventType EventType, handler Handler) error

	// SubscribeOnce registers a handler that will be called only once
	SubscribeOnce(eventType EventType, handler Handler) error

	// Unsubscribe removes a handler for a specific event type
	Unsubscribe(eventType EventType, handler Handler) error
}
