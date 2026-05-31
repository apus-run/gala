package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"
)

// EventBus manages event subscriptions and publishing
type EventBus interface {
	// Subscribe registers a handler for a specific event type
	Subscribe(eventType string, handler Handler) error

	// SubscribeAsync registers an async handler for a specific event type
	SubscribeAsync(eventType string, handler Handler) error

	// SubscribeOnce registers a handler that will be called only once
	SubscribeOnce(eventType string, handler Handler) error

	// Unsubscribe removes a handler for a specific event type
	Unsubscribe(eventType string, handler Handler) error

	// Publish publishes an event to all subscribed handlers
	Publish(ctx context.Context, event *Event) error

	// PublishAsync publishes an event asynchronously
	PublishAsync(ctx context.Context, event *Event) error

	// Close closes the event bus and cleans up resources
	Close() error
}

// DefaultEventBus is the default implementation of EventBus
type DefaultEventBus struct {
	mu           sync.RWMutex
	handlers     map[string][]Handler
	onceHandlers map[string][]Handler
	logger       *slog.Logger
	closed       bool
}

// NewEventBus creates a new event bus
func NewEventBus(logger *slog.Logger) EventBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultEventBus{
		handlers:     make(map[string][]Handler),
		onceHandlers: make(map[string][]Handler),
		logger:       logger.With("module", "eventbus"),
	}
}

// Subscribe registers a handler for a specific event type
func (eb *DefaultEventBus) Subscribe(eventType string, handler Handler) error {
	if err := validateSubscription(eventType, handler); err != nil {
		return err
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return fmt.Errorf("event bus is closed")
	}

	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
	return nil
}

// SubscribeAsync registers an async handler for a specific event type
func (eb *DefaultEventBus) SubscribeAsync(eventType string, handler Handler) error {
	if err := validateSubscription(eventType, handler); err != nil {
		return err
	}
	asyncHandler := NewAsyncHandler(handler)
	return eb.Subscribe(eventType, asyncHandler)
}

// SubscribeOnce registers a handler that will be called only once
func (eb *DefaultEventBus) SubscribeOnce(eventType string, handler Handler) error {
	if err := validateSubscription(eventType, handler); err != nil {
		return err
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return fmt.Errorf("event bus is closed")
	}

	eb.onceHandlers[eventType] = append(eb.onceHandlers[eventType], handler)
	return nil
}

// Unsubscribe removes a handler for a specific event type
func (eb *DefaultEventBus) Unsubscribe(eventType string, handler Handler) error {
	if err := validateSubscription(eventType, handler); err != nil {
		return err
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	if removeHandler(eb.handlers, eventType, handler) {
		return nil
	}
	if removeHandler(eb.onceHandlers, eventType, handler) {
		return nil
	}

	return fmt.Errorf("handler not found for event type: %s", eventType)
}

// Publish publishes an event to all subscribed handlers
func (eb *DefaultEventBus) Publish(ctx context.Context, event *Event) error {
	if ctx == nil {
		return fmt.Errorf("publish context is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	eb.mu.Lock()
	if eb.closed {
		eb.mu.Unlock()
		eb.logger.Warn("Event bus is closed, cannot publish event", "type", event.Type)
		return fmt.Errorf("event bus is closed")
	}

	handlers := append([]Handler(nil), eb.handlers[event.Type]...)
	onceHandlers := eb.onceHandlers[event.Type]
	if len(onceHandlers) > 0 {
		delete(eb.onceHandlers, event.Type)
	}
	eb.mu.Unlock()

	if len(handlers) == 0 && len(onceHandlers) == 0 {
		return nil
	}

	var errs []error
	for _, handler := range handlers {
		if err := handler.Handle(ctx, event); err != nil {
			eb.logger.Error("Handler error", "type", event.Type, "error", err)
			errs = append(errs, err)
		}
	}

	for _, handler := range onceHandlers {
		if err := handler.Handle(ctx, event); err != nil {
			eb.logger.Error("Once handler error", "type", event.Type, "error", err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// PublishAsync publishes an event asynchronously
// Uses a fresh background context so the goroutine isn't affected by caller context cancellation
func (eb *DefaultEventBus) PublishAsync(ctx context.Context, event *Event) error {
	if ctx == nil {
		return fmt.Errorf("publish context is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := eb.Publish(bgCtx, event); err != nil {
			eb.logger.Error("Async publish error", "type", event.Type, "error", err)
		}
	}()
	return nil
}

// Close closes the event bus and cleans up resources
func (eb *DefaultEventBus) Close() error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return fmt.Errorf("event bus already closed")
	}

	eb.closed = true
	eb.handlers = make(map[string][]Handler)
	eb.onceHandlers = make(map[string][]Handler)
	eb.logger.Info("Event bus closed")

	return nil
}

// GetSubscriberCount returns the number of subscribers for an event type
func (eb *DefaultEventBus) GetSubscriberCount(eventType string) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return len(eb.handlers[eventType]) + len(eb.onceHandlers[eventType])
}

// GetEventTypes returns all event types that have subscribers
func (eb *DefaultEventBus) GetEventTypes() []string {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	types := make(map[string]struct{})
	for eventType := range eb.handlers {
		types[eventType] = struct{}{}
	}
	for eventType := range eb.onceHandlers {
		types[eventType] = struct{}{}
	}

	result := make([]string, 0, len(types))
	for eventType := range types {
		result = append(result, eventType)
	}

	return result
}

func validateSubscription(eventType string, handler Handler) error {
	if eventType == "" {
		return fmt.Errorf("event type is empty")
	}
	if isNilHandler(handler) {
		return fmt.Errorf("handler is nil")
	}
	return nil
}

func removeHandler(handlersByType map[string][]Handler, eventType string, handler Handler) bool {
	handlers := handlersByType[eventType]
	for i, candidate := range handlers {
		if sameHandler(candidate, handler) {
			handlers = append(handlers[:i], handlers[i+1:]...)
			if len(handlers) == 0 {
				delete(handlersByType, eventType)
			} else {
				handlersByType[eventType] = handlers
			}
			return true
		}
	}
	return false
}

func isNilHandler(handler Handler) bool {
	if handler == nil {
		return true
	}

	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func sameHandler(left, right Handler) bool {
	if isNilHandler(left) || isNilHandler(right) {
		return isNilHandler(left) && isNilHandler(right)
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() {
		return false
	}

	switch leftValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		return leftValue.Type().Comparable() && leftValue.Interface() == rightValue.Interface()
	}
}
