package hook

var _ Resolver = (*Event)(nil)

// Resolver defines a common interface for a Hook event (see [Event]).
//
// It can only be implemented by embedding [Event].
// Resolver 定义 Hook 事件的公共接口（见 [Event]），只能通过内嵌 [Event] 实现。
type Resolver interface {
	// Next triggers the next handler in the hook's chain (if any).
	//
	// It is a no-op (returns nil) when there is no next handler or when
	// the event is not currently being triggered.
	// Next 触发钩子链中的下一个处理器（如有）。
	// 没有下一个处理器或事件未在触发中时为空操作（返回 nil）。
	Next() error

	// note: kept only for the generic interface; may get removed in the future
	// 注意：仅为泛型接口保留；将来可能移除
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
// Event 实现 [Resolver]，用作基础 Hook 事件，可内嵌到自定义类型化事件结构体。
//
// 事件保存当前在其上执行的链条状态，因此不能同时在多个 goroutine 中触发
// 同一事件（每个 goroutine 用独立事件）。支持在同一事件自身的处理器内再次
// 触发，或先后多次触发。
type Event struct {
	next func() error
}

// Next calls the next hook handler.
// Next 调用下一个钩子处理器。
func (e *Event) Next() error {
	if e.next != nil {
		return e.next()
	}
	return nil
}

// nextFunc returns the function that Next calls.
// nextFunc 返回 Next 所调用的函数。
func (e *Event) nextFunc() func() error {
	return e.next
}

// setNextFunc sets the function that Next calls.
// setNextFunc 设置 Next 所调用的函数。
func (e *Event) setNextFunc(f func() error) {
	e.next = f
}
