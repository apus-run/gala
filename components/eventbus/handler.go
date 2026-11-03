package eventbus

import "context"

// EventHandler is a function that handles an event | EventHandler 是处理事件的函数。
type EventHandler func(ctx context.Context, event *Event) error

// EventHandlerFunc is an adapter to allow ordinary functions to be used as EventHandlers | EventHandlerFunc 让普通函数可用作 EventHandler。
type EventHandlerFunc func(ctx context.Context, event *Event) error

// Handle calls the handler function | Handle 调用该 handler 函数。
func (f EventHandlerFunc) Handle(ctx context.Context, event *Event) error {
	return f(ctx, event)
}

// Handler interface for event handlers | Handler 是事件 handler 的接口。
type Handler interface {
	Handle(ctx context.Context, event *Event) error
}

// ChainHandler chains multiple handlers together | ChainHandler 将多个 handler 串联起来。
type ChainHandler struct {
	handlers []Handler
}

// NewChainHandler creates a new chain handler | NewChainHandler 创建新的链式 handler。
func NewChainHandler(handlers ...Handler) *ChainHandler {
	return &ChainHandler{handlers: handlers}
}

// Handle executes all handlers in sequence | Handle 顺序执行所有 handler。
func (h *ChainHandler) Handle(ctx context.Context, event *Event) error {
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// FilterHandler wraps a handler with a filter function | FilterHandler 用过滤函数包装 handler。
type FilterHandler struct {
	filter  func(*Event) bool
	handler Handler
}

// NewFilterHandler creates a new filter handler | NewFilterHandler 创建新的过滤 handler。
func NewFilterHandler(filter func(*Event) bool, handler Handler) *FilterHandler {
	return &FilterHandler{
		filter:  filter,
		handler: handler,
	}
}

// Handle executes the handler only if the filter returns true | Handle 仅在过滤器返回 true 时执行 handler。
func (h *FilterHandler) Handle(ctx context.Context, event *Event) error {
	if h.filter(event) {
		return h.handler.Handle(ctx, event)
	}
	return nil
}
