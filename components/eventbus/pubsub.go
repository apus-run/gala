package eventbus

import "context"

// PubSub combines event publishing and subscription operations.
type PubSub interface {
	Publisher
	Subscriber
}

type Publisher interface {
	// Publish publishes an event to all subscribed handlers
	Publish(ctx context.Context, event *Event) error

	// PublishAsync publishes an event asynchronously
	PublishAsync(ctx context.Context, event *Event) error

	Close() error
}

type Subscriber interface {
	// Subscribe registers a handler for a specific event type
	Subscribe(eventType EventType, handler Handler) error

	// SubscribeAsync registers an async handler for a specific event type
	SubscribeAsync(eventType EventType, handler Handler) error

	// SubscribeOnce registers a handler that will be called only once
	SubscribeOnce(eventType EventType, handler Handler) error

	// Unsubscribe removes a handler for a specific event type
	Unsubscribe(eventType EventType, handler Handler) error

	Close() error
}
