package eventbus

import (
	"context"
	"io"
)

// PubSub combines event publishing and subscription operations | PubSub 组合事件的发布与订阅操作。
type PubSub interface {
	Publisher
	Subscriber
	io.Closer
}

// Publisher publishes events to subscribed handlers | Publisher 向已订阅的 handler 发布事件。
type Publisher interface {
	// Publish executes all subscribed handlers sequentially and aggregates errors | Publish 顺序执行所有已订阅的 handler 并聚合错误。
	Publish(ctx context.Context, event *Event) error

	// PublishAsync dispatches handlers with bounded concurrency and returns after
	// every handler has been scheduled. Handler failures are logged.
	// PublishAsync 以受限并发分发 handler，全部调度完成后返回；handler 失败只记日志。
	PublishAsync(ctx context.Context, event *Event) error

	// PublishAndWait executes handlers with bounded concurrency, waits for all of
	// them, and aggregates errors.
	// PublishAndWait 以受限并发执行 handler，等待全部完成后聚合错误。
	PublishAndWait(ctx context.Context, event *Event) error
}

// Subscriber subscribes handlers to event types | Subscriber 为事件类型订阅 handler。
type Subscriber interface {
	// Subscribe registers a handler for a specific event type | Subscribe 为指定事件类型注册 handler。
	Subscribe(eventType EventType, handler Handler) error

	// SubscribeWithCancel registers a handler and returns an identity-safe cancel function | SubscribeWithCancel 注册 handler 并返回身份安全的取消函数。
	SubscribeWithCancel(eventType EventType, handler Handler) (cancel func() error, err error)

	// SubscribeOnce registers a handler that will be called only once | SubscribeOnce 注册只被调用一次的 handler。
	SubscribeOnce(eventType EventType, handler Handler) error

	// SubscribeOnceWithCancel registers a one-shot handler with identity-safe cancellation | SubscribeOnceWithCancel 注册带身份安全取消的一次性 handler。
	SubscribeOnceWithCancel(eventType EventType, handler Handler) (cancel func() error, err error)

	// Unsubscribe removes a handler whose identity is safely comparable. Function
	// handlers must use the cancel function returned by SubscribeWithCancel.
	// Unsubscribe 移除身份可安全比较的 handler。函数 handler 必须使用
	// SubscribeWithCancel 返回的取消函数。
	Unsubscribe(eventType EventType, handler Handler) error
}
