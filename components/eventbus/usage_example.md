# eventbus 使用示例

## 创建与关闭

```go
logger := slog.Default()
bus := eventbus.NewEventBus(logger)
defer bus.Close()
```

默认并发上限为 64。高吞吐场景可以显式设置：

```go
bus := eventbus.NewEventBusWithConcurrency(logger, 32)
defer bus.Close()
```

## 订阅与同步发布

```go
const EventOrderCreated eventbus.EventType = "order.created"

type OrderCreated struct {
    OrderID string `json:"order_id"`
}

err := bus.Subscribe(EventOrderCreated, eventbus.EventHandlerFunc(
    func(ctx context.Context, event *eventbus.Event) error {
        data := event.Data.(OrderCreated)
        slog.InfoContext(ctx, "order created", "order_id", data.OrderID)
        return nil
    },
))
if err != nil {
    return err
}

return bus.Publish(ctx, eventbus.NewEvent(EventOrderCreated, OrderCreated{
    OrderID: "order-42",
}))
```

`Publish` 会执行全部 handler。多个错误通过 `errors.Join` 聚合，因此可以用 `errors.Is` 和 `errors.As` 检查。

需要取消函数 handler 时，使用 identity-safe cancel：

```go
cancel, err := bus.SubscribeWithCancel(EventOrderCreated, handler)
if err != nil {
    return err
}
defer cancel()
```

不要用 `Unsubscribe` 猜测闭包身份；Go 的函数值不可比较。

## 有界异步发布

```go
ctx, cancel := context.WithTimeout(parent, 200*time.Millisecond)
defer cancel()

err := bus.PublishAsync(ctx, eventbus.NewEvent(EventOrderCreated, payload))
if err != nil {
    // 并发槽满且 Context 结束时返回 Context 错误。
    return err
}
```

`PublishAsync` 返回时表示所有 handler 已派发，不表示已经完成。handler error 和 panic 会写入 bus logger；`Close` 会等待已派发任务结束。

## 并发发布并等待

```go
err := bus.PublishAndWait(ctx, eventbus.NewEvent(EventOrderCreated, payload))
if err != nil {
    return fmt.Errorf("notify order creation: %w", err)
}
```

该方法适合彼此独立、调用方又必须等待结果的 handler。

## 一次性订阅

```go
err := bus.SubscribeOnce("application.ready", eventbus.EventHandlerFunc(
    func(ctx context.Context, event *eventbus.Event) error {
        return warmCache(ctx)
    },
))
```

一次性 handler 在首次发布开始时从订阅表中移除，并发发布也只会执行一次。

## 中间件

```go
handler := eventbus.EventHandlerFunc(processOrder)
wrapped := eventbus.Chain(
    eventbus.LoggingMiddleware(logger),
    eventbus.RecoveryMiddleware(logger),
    eventbus.TimeoutMiddleware(2*time.Second),
    eventbus.RetryMiddleware(2, 50*time.Millisecond),
)(handler)

if err := bus.Subscribe(EventOrderCreated, wrapped); err != nil {
    return err
}
```

超时是协作式的。handler 应把 Context 继续传给数据库、HTTP 等下游，并主动响应 `ctx.Done()`。

并发 handler 会收到独立的 Event 与 Metadata，但 Data 必须视为不可变。若 Data 包含 map、slice 或 pointer 且需要修改，handler 应先复制。

## Manager

```go
manager := eventbus.NewManager(logger)
defer manager.Close()

if err := manager.Subscribe("orders", EventOrderCreated, handler); err != nil {
    return err
}

if err := manager.PublishAsync(ctx, "orders", event); err != nil {
    return err
}

return manager.PublishGlobalAndWait(ctx, auditEvent)
```

Manager 同时提供同步、异步和并发等待版本。`Close` 会聚合各 bus 的关闭错误。

## 选择边界

`eventbus` 只用于进程内通信。需要跨进程、持久化、确认、重放或严格投递保证时，应使用 MQ/stream，而不是继续扩展内存 EventBus。
