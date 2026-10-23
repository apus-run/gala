package hook

var _ Resolver = (*Event)(nil)

// Resolver defines a common interface for a Hook event (see [Event]).
//
// It can only be implemented by embedding [Event].
type Resolver interface {
	// Next triggers the next handler in the hook's chain (if any).
	//
	// It is a no-op (returns nil) when there is no next handler or when
	// the event is not currently being triggered.
	Next() error

	// note: kept only for the generic interface; may get removed in the future
	nextFunc() func() error
	setNextFunc(f func() error)
}

// Event implements [Resolver] and it is intended to be used as a base
// Hook event that you can embed in your custom typed event structs.
//
// Example:
//
//	type CustomEvent struct {
//		hook.Event
//
//		SomeField int
//	}
//
// An event holds the state of the chain that is currently executed on it,
// so it must not be triggered from multiple goroutines at the same time
// (use a separate event per goroutine). Triggering the same event again
// from inside one of its own handlers, or one after another, is supported.
type Event struct {
	next func() error
}

// Next calls the next hook handler.
func (e *Event) Next() error {
	if e.next != nil {
		return e.next()
	}
	return nil
}

// nextFunc returns the function that Next calls.
func (e *Event) nextFunc() func() error {
	return e.next
}

// setNextFunc sets the function that Next calls.
func (e *Event) setNextFunc(f func() error) {
	e.next = f
}
