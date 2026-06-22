# Gala EventBus 事件系统总结

## 概述

`components/eventbus` 是 Gala 的进程内发布—订阅组件，用于在同一进程的模块之间传递事件，降低发送者与处理者之间的直接依赖。

它统一承载同步通知、有界异步派发、并发等待、订阅管理、中间件和多总线管理。原 `components/event` 已完成能力迁移并移除，仓库只保留这一套进程内事件机制。

EventBus 不提供跨进程传输、持久化、确认、重放或严格投递保证。需要这些能力时应使用 MQ、stream 或数据库 outbox，而不是继续扩展内存总线。

## 核心功能

### ✅ 已实现能力

1. **三种发布方式**
   - `Publish`：按订阅顺序同步执行全部 handler。
   - `PublishAsync`：使用共享并发上限异步派发。
   - `PublishAndWait`：有界并发执行并等待全部 handler。

2. **订阅管理**
   - `Subscribe`：注册普通 handler。
   - `SubscribeOnce`：注册一次性 handler。
   - `SubscribeWithCancel`：注册并获得可靠的取消函数。
   - `SubscribeOnceWithCancel`：可取消的一次性订阅。
   - `Unsubscribe`：取消身份可安全比较的对象 handler。

3. **错误与 panic 隔离**
   - 同步和等待式发布通过 `errors.Join` 聚合 handler error。
   - handler panic 统一转换为 `*PanicError`。
   - 异步 handler 的 error 和 panic 写入结构化日志。
   - 单个 handler 失败不会阻止其他 handler 执行。

4. **并发与背压**
   - 默认最多并发执行 64 个 handler。
   - `NewEventBusWithConcurrency` 可调整并发上限。
   - 并发槽满时根据 Context 等待或取消派发。
   - handler 重入并发发布时返回 `ErrReentrantPublish`，避免等待自己占用的并发槽。

5. **生命周期**
   - `Close` 拒绝后续订阅与发布。
   - `Close` 等待已经开始的同步、异步和等待式发布完成。
   - Manager 关闭时聚合每个 bus 的关闭错误。
   - Manager 关闭后返回稳定的 `ErrManagerClosed`。

6. **中间件**
   - `LoggingMiddleware`：结构化处理日志。
   - `RecoveryMiddleware`：独立包装 handler 的 panic 恢复。
   - `TimeoutMiddleware`：基于 Context 的协作式超时。
   - `RetryMiddleware`：带延迟的失败重试。
   - `MetricsMiddleware`：记录处理时长和成功状态。
   - `Chain`：按声明顺序组合多个中间件。

7. **Handler 组合**
   - `EventHandlerFunc`：将普通函数适配为 Handler。
   - `ChainHandler`：串行组合多个 handler。
   - `FilterHandler`：按 Event 条件过滤。

8. **多总线管理**
   - `Manager.GetBus`：按名称获取或创建隔离 bus。
   - `Manager.Global`：获取 Manager 自身的 global bus。
   - 同步、异步和等待式发布均提供 named/global 入口。
   - 不使用 package 级可变全局单例。

## 架构结构

```text
Publisher
    │
    ▼
EventBus ── EventType ──► []Handler
    │                         │
    │                         ├── Middleware(Handler)
    │                         ├── ChainHandler
    │                         └── FilterHandler
    │
    ├── Publish          顺序执行、聚合错误
    ├── PublishAsync     有界异步、日志记录错误
    ├── PublishAndWait   有界并发、等待并聚合错误
    └── Close            拒绝新任务并 drain

Manager
    ├── Global Bus
    └── Named Buses: map[string]PubSub
```

组件只有 Go 标准库依赖，不依赖数据库、网络或外部消息系统。

## 文件结构

```text
components/eventbus/
├── event.go                  # Event、EventType、ID 与 Metadata
├── eventbus.go               # 订阅、发布、并发控制和生命周期
├── pubsub.go                 # Publisher、Subscriber、PubSub 接口
├── handler.go                # Handler 适配、链式和过滤处理器
├── middleware.go             # 日志、恢复、超时、重试和指标
├── manager.go                # named/global bus 管理
├── event_test.go             # Event 模型测试
├── eventbus_test.go          # 基础订阅与发布测试
├── publish_modes_test.go     # 并发、背压、重入和生命周期测试
├── api_test.go               # Handler、中间件和 Manager 测试
├── example_test.go           # 可执行外部包示例
├── README.md                 # API 说明
├── INTEGRATION.md            # 项目集成说明
├── usage_example.md          # 可复制使用示例
└── SUMMARY.md                # 能力、架构与边界总览
```

领域事件常量和 DTO 不放在 eventbus 中。Email、User、Order 等事件应由对应业务 package 定义；`example_test.go` 中的 Email 常量只用于演示。

## 发布语义

| API | 执行模型 | Handler 错误 | 返回时机 | 典型用途 |
|---|---|---|---|---|
| `Publish` | 顺序 | 聚合返回 | 全部完成 | 有顺序要求、逻辑简单 |
| `PublishAsync` | 有界并发 | 结构化日志 | 全部已派发 | 非关键通知、审计、缓存失效 |
| `PublishAndWait` | 有界并发 | 聚合返回 | 全部完成 | 多个独立任务且调用方需要结果 |

### 同步发布

```go
err := bus.Publish(ctx, eventbus.NewEvent(EventOrderCreated, payload))
if err != nil {
    return fmt.Errorf("publish order event: %w", err)
}
```

所有 handler 都会执行。返回值可能同时包装多个错误，可使用 `errors.Is` 和 `errors.As` 检查。

### 有界异步发布

```go
bus := eventbus.NewEventBusWithConcurrency(logger, 32)
defer bus.Close()

ctx, cancel := context.WithTimeout(parent, 200*time.Millisecond)
defer cancel()

if err := bus.PublishAsync(ctx, event); err != nil {
    return err
}
```

返回 nil 表示全部 handler 已成功派发，不代表处理已经完成。应用退出前调用 `Close`，EventBus 会等待已经派发的任务结束。

### 并发执行并等待

```go
if err := bus.PublishAndWait(ctx, event); err != nil {
    return fmt.Errorf("wait event handlers: %w", err)
}
```

适合彼此独立的 handler。并发执行时，每个 handler 收到独立的 Event 和 Metadata 副本。

## Event 所有权

`Event` 包含 ID、类型、来源、Data、Metadata、时间戳和优先级。

```go
event := eventbus.NewEvent(EventOrderCreated, OrderCreated{
    OrderID: "order-42",
}).WithSource("order-service").WithMetadata("trace_id", traceID)
```

并发发布遵循以下规则：

- Event 结构体按 handler 复制。
- Metadata map 按 handler 克隆。
- Data 是泛型载荷，EventBus 无法安全深拷贝，handler 必须将其视为不可变。
- 需要修改 map、slice 或 pointer Data 时，handler 应自行复制或使用同步机制。
- handler 必须继续传递收到的 Context，不能随意替换为 `context.Background()`。

## 订阅与取消

### 普通订阅

```go
err := bus.Subscribe(EventOrderCreated, eventbus.EventHandlerFunc(
    func(ctx context.Context, event *eventbus.Event) error {
        data := event.Data.(OrderCreated)
        return indexOrder(ctx, data.OrderID)
    },
))
```

### 推荐的函数取消方式

Go 的函数值不可比较，不能依靠代码指针可靠识别闭包。函数 handler 应保存注册时返回的 cancel：

```go
cancel, err := bus.SubscribeWithCancel(EventOrderCreated, handler)
if err != nil {
    return err
}
defer cancel()
```

`Unsubscribe(eventType, handler)` 只适用于身份可安全比较的对象 handler。对函数 handler 调用它会返回 `ErrUnstableHandlerIdentity`。

### 一次性订阅

```go
cancel, err := bus.SubscribeOnceWithCancel("application.ready", warmCacheHandler)
```

一次性 handler 在首次成功派发后消费。Context 在派发途中取消时，尚未派发的一次性 handler 会恢复到订阅表。

## 中间件组合

```go
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

中间件按声明顺序从外向内执行。推荐顺序为 Logging → Recovery → Timeout → Retry → Handler。

`TimeoutMiddleware` 是协作式超时，不会创建用于“强制超时”的孤儿 goroutine。被包装 handler 必须观察 `ctx.Done()`，并把 Context 传给数据库、HTTP 等下游。

无效配置会尽早 panic：

- timeout 必须大于 0；
- maxRetries 不能为负数；
- retry delay 不能为负数。

## Manager 集成

### 初始化

```go
manager := eventbus.NewManager(logger)
defer manager.Close()
```

### 命名总线

```go
orders, err := manager.GetBus("orders")
if err != nil {
    return err
}

if err := orders.Subscribe(EventOrderCreated, handler); err != nil {
    return err
}
```

### Manager 便捷入口

```go
if err := manager.Subscribe("orders", EventOrderCreated, handler); err != nil {
    return err
}

if err := manager.PublishAsync(ctx, "orders", event); err != nil {
    return err
}
```

### Global Bus

Global 仅属于当前 Manager 实例，并非 package 级单例：

```go
global, err := manager.Global()
if err != nil {
    return err
}
```

Manager 关闭后，`GetBus`、`Global` 和发布/订阅入口返回 `ErrManagerClosed`。

## 典型使用场景

### 1. 缓存失效

业务提交成功后发布领域事件，由独立 handler 清理缓存，避免业务服务直接依赖缓存实现。

### 2. 审计与行为记录

使用 `PublishAsync` 将非关键审计记录与主流程解耦。异步失败通过结构化日志和监控观察。

### 3. 多个并行派生任务

使用 `PublishAndWait` 并行更新搜索索引、统计和本地投影，并在返回前聚合错误。

### 4. 应用初始化

使用 `SubscribeOnce` 处理 `application.ready` 等只需执行一次的进程事件。

### 5. 模块隔离

通过 Manager 的 named bus 隔离 orders、email、agent 等事件域，避免所有订阅都堆在单一总线上。

## 错误处理

| 错误 | 含义 | 建议处理 |
|---|---|---|
| `ErrReentrantPublish` | handler 使用同一 Context 重入并发发布 | 改为同步发布、拆分流程或在 handler 返回后发布 |
| `ErrUnstableHandlerIdentity` | 尝试按函数值取消订阅 | 使用 `SubscribeWithCancel` 返回的 cancel |
| `ErrManagerClosed` | Manager 已关闭 | 停止创建和发布事件 |
| `*PanicError` | handler 发生 panic | 记录并修复 handler；不要将 panic 当普通业务错误 |
| `*TimeoutError` | handler 超过协作式 deadline | 确认 handler 正确传播 Context |

## 并发安全与限制

### 已保证

- 注册、取消和发布时的订阅表访问受锁保护。
- 一次性订阅在并发发布时最多派发一次。
- 异步 goroutine 数量受配置上限约束。
- Close 与新发布之间有明确状态边界。
- Close 会等待已经开始的发布完成。

### 调用方责任

- Data 必须不可变或自行同步。
- handler 必须传播 Context。
- 不应在 handler 中使用 `context.Background()` 重入同一 bus。
- `PublishAsync` 不保证持久化、重试或进程崩溃后的交付。
- EventBus 不是跨服务消息系统。

## 测试与质量

当前测试覆盖以下行为：

- 同步发布继续执行并聚合错误；
- panic 恢复；
- 有界异步背压；
- 并发等待上限；
- Close drain；
- 重入发布拒绝；
- Event/Metadata 隔离；
- 一次性订阅回滚；
- identity-safe 取消订阅；
- Manager 关闭状态和锁顺序；
- 中间件参数校验；
- Event ID 唯一性抽样。

最近验证结果：

- `go test -race -count=30 ./...` 通过；
- `go vet ./...` 通过；
- 语句覆盖率约 82.1%；
- 无第三方运行时依赖。

仓库当前没有稳定 benchmark，因此不在文档中承诺具体纳秒级性能。需要性能结论时应针对实际 handler 数、并发上限和 payload 编写 benchmark。

## 优势

1. **发送者与处理者解耦**：业务模块只依赖事件契约。
2. **执行模型明确**：同步、异步和等待式发布分开表达。
3. **资源有界**：并发上限和 Context 共同控制过载。
4. **失败隔离**：error 聚合、panic 转换、异步日志可观测。
5. **生命周期完整**：Close 拒绝新任务并 drain。
6. **测试友好**：无 package 全局单例，可为每个测试创建独立 bus。
7. **领域边界清晰**：基础设施不内置 Email/User/Task DTO。

## 集成步骤

### 步骤 1：在 Composition Root 创建 Manager

```go
manager := eventbus.NewManager(logger)
```

将 `eventbus.Publisher`、`eventbus.Subscriber` 或更小的业务接口注入服务，不要让业务代码自行创建全局单例。

### 步骤 2：在领域 package 定义事件

```go
const EventOrderCreated eventbus.EventType = "order.created"

type OrderCreated struct {
    OrderID string
}
```

### 步骤 3：应用启动时注册 handler

注册应集中在 wiring/container 层，便于观察订阅关系和统一应用中间件。

### 步骤 4：业务提交后发布

先完成数据库事务，再发布进程内通知。若要求事务与事件严格一致，应使用 outbox，而不是依赖内存 EventBus。

### 步骤 5：应用退出时 Close

```go
if err := manager.Close(); err != nil {
    logger.Error("close event manager", "error", err)
}
```

## 后续建议

1. 将 Markdown 中最关键的代码块继续转为可执行 `Example`，降低文档漂移风险。
2. 鼓励消费者依赖 `Publisher`、`Subscriber` 等窄接口，而不是完整 `PubSub`。
3. 根据真实调用量补充 benchmark 和队列等待指标。
4. 为异步失败、背压等待和 handler duration 接入统一 metrics。
5. 如果出现可靠投递需求，新增 MQ/outbox adapter，而不是改变内存总线语义。

## 相关文档

- [README.md](README.md)：API 与基础说明。
- [usage_example.md](usage_example.md)：常见调用方式。
- [INTEGRATION.md](INTEGRATION.md)：项目集成示例。
- [example_test.go](example_test.go)：可执行外部包示例。
- [publish_modes_test.go](publish_modes_test.go)：并发和生命周期契约。

## 总结

Gala EventBus 已形成一套单一、进程内、有界并发的事件机制。它适合模块解耦、非关键异步通知和多个派生任务协作；同时通过明确的 Event 所有权、Context 传播和生命周期约束避免常见的共享状态与 goroutine 问题。

正确使用它的关键不是“把所有调用都改成事件”，而是在需要解耦且允许进程内投递语义的边界使用，并在可靠性需求升级时及时切换到专门的消息基础设施。
