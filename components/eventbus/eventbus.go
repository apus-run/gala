package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"
)

const defaultMaxConcurrency = 64

var (
	ErrReentrantPublish        = errors.New("eventbus: reentrant concurrent publish")
	ErrUnstableHandlerIdentity = errors.New("eventbus: function handler identity is unstable; use SubscribeWithCancel")
)

type dispatchContextKey struct{}

type subscriptionHandler struct {
	handler Handler
}

func (h *subscriptionHandler) Handle(ctx context.Context, event *Event) error {
	return h.handler.Handle(ctx, event)
}

var _ PubSub = (*EventBus)(nil)

// EventBus is the default in-memory implementation of PubSub.
type EventBus struct {
	mu           sync.RWMutex
	handlers     map[EventType][]Handler
	onceHandlers map[EventType][]Handler
	logger       *slog.Logger
	asyncSlots   chan struct{}
	asyncWG      sync.WaitGroup
	dispatchWG   sync.WaitGroup
	closed       bool
}

// NewEventBus creates a new event bus
func NewEventBus(logger *slog.Logger) PubSub {
	return NewEventBusWithConcurrency(logger, defaultMaxConcurrency)
}

// NewEventBusWithConcurrency creates an event bus with a shared concurrency
// limit for PublishAsync and PublishAndWait.
func NewEventBusWithConcurrency(logger *slog.Logger, maxConcurrent int) PubSub {
	if logger == nil {
		logger = slog.Default()
	}
	if maxConcurrent <= 0 {
		panic("eventbus: max concurrency must be positive")
	}
	return &EventBus{
		handlers:     make(map[EventType][]Handler),
		onceHandlers: make(map[EventType][]Handler),
		logger:       logger.With("module", "eventbus"),
		asyncSlots:   make(chan struct{}, maxConcurrent),
	}
}

// Subscribe registers a handler for a specific event type
func (eb *EventBus) Subscribe(eventType EventType, handler Handler) error {
	return eb.subscribe(eventType, handler, false)
}

// SubscribeWithCancel registers a handler and returns an identity-safe cancel function.
func (eb *EventBus) SubscribeWithCancel(eventType EventType, handler Handler) (func() error, error) {
	wrapped := &subscriptionHandler{handler: handler}
	if err := eb.subscribe(eventType, wrapped, false); err != nil {
		return nil, err
	}
	return cancelSubscription(eb, eventType, wrapped), nil
}

func (eb *EventBus) subscribe(eventType EventType, handler Handler, once bool) error {
	if err := validateSubscription(eventType, handler); err != nil {
		return err
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.closed {
		return fmt.Errorf("event bus is closed")
	}

	if once {
		eb.onceHandlers[eventType] = append(eb.onceHandlers[eventType], handler)
	} else {
		eb.handlers[eventType] = append(eb.handlers[eventType], handler)
	}
	return nil
}

// SubscribeOnce registers a handler that will be called only once
func (eb *EventBus) SubscribeOnce(eventType EventType, handler Handler) error {
	return eb.subscribe(eventType, handler, true)
}

// SubscribeOnceWithCancel registers a one-shot handler with safe cancellation.
func (eb *EventBus) SubscribeOnceWithCancel(eventType EventType, handler Handler) (func() error, error) {
	wrapped := &subscriptionHandler{handler: handler}
	if err := eb.subscribe(eventType, wrapped, true); err != nil {
		return nil, err
	}
	return cancelSubscription(eb, eventType, wrapped), nil
}

// Unsubscribe removes a handler for a specific event type
func (eb *EventBus) Unsubscribe(eventType EventType, handler Handler) error {
	if err := validateSubscription(eventType, handler); err != nil {
		return err
	}
	if reflect.ValueOf(handler).Kind() == reflect.Func {
		return ErrUnstableHandlerIdentity
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
func (eb *EventBus) Publish(ctx context.Context, event *Event) error {
	handlers, _, err := eb.takeHandlers(ctx, event, false, true)
	if err != nil {
		return err
	}
	defer eb.dispatchWG.Done()
	var errs []error
	for _, handler := range handlers {
		if err := eb.callHandler(ctx, event, handler); err != nil {
			eb.logger.ErrorContext(ctx, "handler failed", "type", event.Type, "error", err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// PublishAsync dispatches handlers using the configured concurrency limit.
// It applies backpressure while all slots are occupied. Handler errors and
// recovered panics are written to the bus logger.
func (eb *EventBus) PublishAsync(ctx context.Context, event *Event) error {
	if dispatchingBus(ctx) == eb {
		return ErrReentrantPublish
	}
	handlers, onceStart, err := eb.takeHandlers(ctx, event, true, false)
	if err != nil {
		return err
	}

	for i, handler := range handlers {
		select {
		case eb.asyncSlots <- struct{}{}:
		case <-ctx.Done():
			eb.restoreOnce(event.Type, remainingOnce(handlers, onceStart, i))
			for range len(handlers) - i {
				eb.asyncWG.Done()
			}
			return fmt.Errorf("dispatch event %s: %w", event.Type, ctx.Err())
		}
		go func() {
			defer eb.asyncWG.Done()
			defer func() { <-eb.asyncSlots }()
			if err := eb.callHandler(ctx, event, handler); err != nil {
				eb.logger.ErrorContext(ctx, "async handler failed", "type", event.Type, "error", err)
			}
		}()
	}
	return nil
}

// PublishAndWait executes handlers concurrently within the configured limit
// and returns after every dispatched handler completes.
func (eb *EventBus) PublishAndWait(ctx context.Context, event *Event) error {
	if dispatchingBus(ctx) == eb {
		return ErrReentrantPublish
	}
	handlers, onceStart, err := eb.takeHandlers(ctx, event, false, true)
	if err != nil {
		return err
	}
	defer eb.dispatchWG.Done()

	var wg sync.WaitGroup
	errCh := make(chan error, len(handlers))
	var dispatchErr error
dispatch:
	for i, handler := range handlers {
		select {
		case eb.asyncSlots <- struct{}{}:
		case <-ctx.Done():
			eb.restoreOnce(event.Type, remainingOnce(handlers, onceStart, i))
			dispatchErr = fmt.Errorf("dispatch event %s: %w", event.Type, ctx.Err())
			break dispatch
		}
		wg.Go(func() {
			defer func() { <-eb.asyncSlots }()
			if err := eb.callHandler(ctx, event, handler); err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)

	errs := make([]error, 0, len(errCh)+1)
	if dispatchErr != nil {
		errs = append(errs, dispatchErr)
	}
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (eb *EventBus) takeHandlers(ctx context.Context, event *Event, reserveAsync, reserveDispatch bool) ([]Handler, int, error) {
	if ctx == nil {
		return nil, 0, fmt.Errorf("publish context is nil")
	}
	if event == nil {
		return nil, 0, fmt.Errorf("event is nil")
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()
	if eb.closed {
		eb.logger.WarnContext(ctx, "event bus is closed", "type", event.Type)
		return nil, 0, fmt.Errorf("event bus is closed")
	}

	handlers := slices.Clone(eb.handlers[event.Type])
	onceStart := len(handlers)
	handlers = append(handlers, eb.onceHandlers[event.Type]...)
	delete(eb.onceHandlers, event.Type)
	if reserveAsync {
		eb.asyncWG.Add(len(handlers))
	}
	if reserveDispatch {
		eb.dispatchWG.Add(1)
	}
	return handlers, onceStart, nil
}

func (eb *EventBus) callHandler(ctx context.Context, event *Event, handler Handler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &PanicError{Value: recovered}
		}
	}()
	ctx = context.WithValue(ctx, dispatchContextKey{}, eb)
	return handler.Handle(ctx, event.Clone())
}

func dispatchingBus(ctx context.Context) *EventBus {
	if ctx == nil {
		return nil
	}
	bus, _ := ctx.Value(dispatchContextKey{}).(*EventBus)
	return bus
}

func remainingOnce(handlers []Handler, onceStart, next int) []Handler {
	return slices.Clone(handlers[max(onceStart, next):])
}

func (eb *EventBus) restoreOnce(eventType EventType, handlers []Handler) {
	if len(handlers) == 0 {
		return
	}
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if !eb.closed {
		eb.onceHandlers[eventType] = append(handlers, eb.onceHandlers[eventType]...)
	}
}

func cancelSubscription(eb *EventBus, eventType EventType, handler Handler) func() error {
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() { err = eb.Unsubscribe(eventType, handler) })
		return err
	}
}

// Close closes the event bus and cleans up resources
func (eb *EventBus) Close() error {
	eb.mu.Lock()
	if eb.closed {
		eb.mu.Unlock()
		return fmt.Errorf("event bus already closed")
	}

	eb.closed = true
	eb.handlers = make(map[EventType][]Handler)
	eb.onceHandlers = make(map[EventType][]Handler)
	eb.mu.Unlock()

	eb.asyncWG.Wait()
	eb.dispatchWG.Wait()
	eb.logger.Info("Event bus closed")

	return nil
}

// GetSubscriberCount returns the number of subscribers for an event type
func (eb *EventBus) GetSubscriberCount(eventType EventType) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return len(eb.handlers[eventType]) + len(eb.onceHandlers[eventType])
}

// GetEventTypes returns all event types that have subscribers
func (eb *EventBus) GetEventTypes() []string {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	types := make(map[EventType]struct{})
	for eventType := range eb.handlers {
		types[eventType] = struct{}{}
	}
	for eventType := range eb.onceHandlers {
		types[eventType] = struct{}{}
	}

	result := make([]string, 0, len(types))
	for eventType := range types {
		result = append(result, string(eventType))
	}
	slices.Sort(result)

	return result
}

func validateSubscription(eventType EventType, handler Handler) error {
	if eventType == "" {
		return fmt.Errorf("event type is empty")
	}
	if isNilHandler(handler) {
		return fmt.Errorf("handler is nil")
	}
	return nil
}

func removeHandler(handlersByType map[EventType][]Handler, eventType EventType, handler Handler) bool {
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
	case reflect.Chan, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		return leftValue.Type().Comparable() && leftValue.Interface() == rightValue.Interface()
	}
}
