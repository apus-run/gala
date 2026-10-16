# layercache

## 基本使用

```go
base, err := layercache.New(
    layercache.WithNamespace("users:v2"),
)
if err != nil { panic(err) }
defer base.Close()

users := layercache.NewTyped[User](base)
u, err := users.GetOrLoad(ctx, "user:42", loadUser)
```

默认提供最多 10,000 条目的本地缓存和 JSON codec，不需要先了解 ecache 或 Entry。需要调整容量时加 `WithLocalCapacity(1000)`；正值条目的 TTL、超时和容量必须为正；负缓存 TTL 为零表示关闭。显式传入的错误值不会被悄悄替换为默认值。

已有 Redis 连接时，添加一个 Remote 就是两级缓存：

```go
remote, err := redisstore.New(redisClient) // 应用管理客户端及网络超时
if err != nil { panic(err) }
base, err := layercache.New(
    layercache.WithNamespace("users:v2"),
    layercache.WithRemote(remote),
)
if err != nil { panic(err) }
defer base.Close()
```

只使用远端时，加 `WithLocal(nil)`。需要自定义本地策略时，传入 `WithLocal(local)`；它和 `WithLocalCapacity(n)` 都选择本地存储，后传的生效。应用创建的后端仍由应用管理生命周期，Cache 不会关闭它们。

`Cache` 保存序列化字节，`Typed[V]` 只负责类型转换；多个视图可以共享一个 Cache，但不同值结构应使用不同 key 域或 namespace。内置 codec 为每个读取者解码独立结果。写入期间不可并发修改入参；写入返回后修改入参不会修改缓存。

JetCache 风格的核心 API：

```go
var user User
err := base.Once(ctx, "user:42",
    layercache.Value(&user), // Once 的输出地址
    layercache.Do(func(ctx context.Context) (any, error) {
        return loadUser(ctx, "user:42")
    }),
    layercache.TTL(time.Minute),
)
err = base.Set(ctx, "user:42", layercache.Value(user)) // Set 的输入值
err = base.Get(ctx, "user:42", &user)                // 不回源
```

可运行示例见 [example_test.go](example_test.go)。

## 接口与选项

```go
func New(opts ...Option) (*Cache, error)
func NewTyped[V any](cache *Cache) *Typed[V]
func NewT[K cmp.Ordered, V any](cache *Cache) *T[K, V]

func (*Cache) Get(ctx context.Context, key string, dst any) error
func (*Cache) GetSkippingLocal(ctx context.Context, key string, dst any) error
func (*Cache) Once(ctx context.Context, key string, opts ...ItemOption) error
func (*Cache) Set(ctx context.Context, key string, opts ...ItemOption) error
func (*Cache) Delete(ctx context.Context, key string) error
func (*Cache) InvalidateLocal(ctx context.Context, key string) error
func (*Cache) Exists(ctx context.Context, key string) (bool, error)
func (*Cache) Stats() Stats
func (*Cache) TaskSize() int
func (*Cache) CacheType() string
func (*Cache) Namespace() string
func (*Cache) SourceID() string
func (*Cache) Close() error
```

Get 的非 nil 目标必须是非 nil 指针；nil 目标只检查条目包，不验证业务 payload。Exists 对 miss/负缓存返回 false，其余错误仍返回给调用方。Once 未提供 Do 且 miss 时返回 ErrMiss。NewTyped/NewT 要求非 nil Cache。

| ItemOption | 语义 |
| --- | --- |
| `Value(v)` | Set 的输入；Once 的输出指针，可为 nil 以预热 |
| `Do(fn)` | 回源函数；Set 同时传 Value/Do 时 Do 优先 |
| `TTL(d)` | 本次正值写入期限，显式值必须大于零 |
| `SetNX(true)` / `SetXX(true)` | 原子条件写，不能同时设置；拒绝返回 ErrNotStored |
| `SkipLocal(true)` | 不读写 L1；Set/Once 需要有 Remote，否则在副作用前拒绝 |
| `Refresh(true)` | 登记该 key 的后台刷新，需要 Do 和全局刷新间隔 |

Typed.Set 的显式值、Typed.GetOrLoad 的 Loader/目标优先于 ItemOption 中的 Value/Do。

| 配置 | 默认与约束 |
| --- | --- |
| `WithNamespace` | 必填，隔离业务 key 域 |
| `WithLocalCapacity` / `WithLocal` | 默认独立内置 L1、10,000 条目；可换为自定义 Local，nil 关闭 L1 |
| `WithRemote` | 默认无 L2；配置后与默认 L1 组成两级，nil 表示不使用 L2 |
| `WithCodec` | 默认标准库 JSON；显式 nil 错误；只要求 Marshal/Unmarshal |
| `WithLocalTTL` / `WithRemoteTTL` | 5s / 5min，有限正数 |
| `WithNotFoundTTL` | `DefaultNotFoundTTL = 0`，关闭负缓存；正值显式开启 |
| `WithErrNotFound` | 识别自定义源不存在错误；不改变负缓存 TTL，与选项顺序无关 |
| `WithTTLJitter` | 0；实际写入 TTL 加 `[0, jitter)`，L1 仍受 LocalTTL 上限约束 |
| `WithLoadTimeout` / `WithRemoteTimeout` | 2s / 250ms |
| `WithMaxConcurrentLoads` | 128，单键、批量和刷新共用任务预算；Get 不计入 |
| `WithErrorHandler` | 自动回填和后台失败回调，默认不打印日志 |
| `WithRefreshInterval` | 0 关闭；开启时至少 1s；有 Remote 时要求 LeaseRemote |
| `WithRefreshIdleTimeout` | 默认刷新间隔 + 1s |
| `WithRefreshConcurrency` / `WithMaxRefreshTasks` | 4 / 10,000 |
| `WithEventHandler` | 传处理器即启用事件，nil 关闭；要求 L1 + L2，不需要额外开关 |
| `WithSourceID` / `WithEventBuffer` | 自动生成唯一 ID / 100；自定义 ID 必须实例间唯一 |
| `WithStatsDisabled` | false；默认提供累计原子计数 |
| `WithStatsHandler` / `WithStatsInterval` | 无处理器 / 1min；配置后周期发送快照 |
| `WithSeparator` | 泛型 ID 门面的分隔符，默认 `:` |

`Option` 是含私有 `apply(*Options)` 方法的接口，`OptionFunc` 是函数适配器；`Options` 字段私有。`DefaultOptions` 每次返回独立默认配置，`NewOptions` / `Apply` 只按顺序组装配置，不校验、不回滚、不创建内置后端。`Validate` 只检查最终配置。`New` 校验成功后保存配置值副本，再创建独立内置本地存储与后台任务。

可以通过公共 API 组合一个可复用配置：

```go
profile := layercache.OptionFunc(func(o *layercache.Options) {
    o.Apply(
        layercache.WithLocalCapacity(1000),
        layercache.WithLocalTTL(10*time.Second),
        layercache.WithRemoteTTL(5*time.Minute),
    )
})
base, err := layercache.New(layercache.WithNamespace("users:v2"), profile)
if err != nil { panic(err) }
defer base.Close()
```

普通配置的默认值来自 `DefaultOptions`；SourceID 和自动刷新空闲期限由 `New` 按实例生成或派生。同一项后传的覆盖先传的。未传 TTL 使用默认值，`WithLocalTTL(0)` 返回明确配置错误；`WithNotFoundTTL(0)` 关闭负缓存，`WithRefreshInterval(0)` 关闭刷新。构造时的 `Option` 与单次操作的 `ItemOption` 分开使用。二者都按调用顺序组装、只校验最终值。`New` 拒绝 nil `Option`，`Set` / `Once` 在解析时拒绝 nil `ItemOption`，返回 `ErrInvalidOptions`；拒绝发生在后端读写、回源或刷新登记之前。`Apply` 仍只接收有效选项。`WithLocal(nil)`、`WithEventHandler(nil)`、`Do(nil)`、`Value(nil)` 返回的是有效选项，nil 是其业务值。

`Option` / `OptionFunc` 操作字段私有的 `Options`；`ItemOption` / `ItemOptionFunc` 操作字段私有的 `ItemOptions`。二者都通过私有 `apply` 方法和公开的 `Apply` 组装配置，不开放字段或运行期修改入口。复用一组 `[]ItemOption` 时，泛型包装器不会改写调用方的底层切片。Typed 的显式参数优先于 Value/Do，这是类型门面的固定规则；NX 与 XX 同时启用仍是明确的配置错误。

单次操作也可以组合成可复用选项：

```go
insert := layercache.ItemOptionFunc(func(o *layercache.ItemOptions) {
    o.Apply(layercache.TTL(time.Minute), layercache.SetNX(true))
})
err := users.Set(ctx, "user:42", user, insert)
```

单次调用先取当前 Cache 的默认 TTL，再按序应用所有选项，最后检查 TTL、NX/XX 及后端能力。`ItemOptions.Apply` 和 `Options.Apply` 一样只组装配置、不校验、不重置、不回滚。每次调用有独立配置值；适配器不得保留配置用于后续修改或并发使用。默认 TTL 和能力检查依赖当前 Cache，因此不增加独立的 `DefaultItemOptions` 或无上下文校验入口。

依赖及回调闭包共享应用对象，必须并发安全。复用选项不会共享内置本地存储；自定义 Local 仍只能交给一个 Cache。所有回调都须及时返回，不得同步重入同一 Cache 或调用 Close。事件为尽力通知，刷新租约不提供数据库事务一致性。

## 调整说明

- `ItemOption` 改为接口，自定义单次选项使用 `ItemOptionFunc(func(*ItemOptions))`；Value、Do、TTL 等工厂的调用方式不变。
- 保留 `New(opts ...Option)` 与 `With…` 范式，`OptionFunc` 改为实现 Option 的函数适配器，不再是别名；自定义选项使用 `OptionFunc`。
- 默认从“必须自行提供后端”改为有容量上限的内置本地缓存。原本只配 Remote、希望保持远端单层的调用，补 `WithLocal(nil)`。
- 统一使用 `WithNamespace`，移除同义入口 `WithName`；`WithStopRefreshAfterLastAccess` 简化为 `WithRefreshIdleTimeout`。
- 移除 `WithSyncLocal`，只用 `WithEventHandler` 启停事件。以前依赖 `WithErrNotFound` 隐式开启负缓存的调用，显式补 `WithNotFoundTTL`。

```bash
go test -race ./...
go vet ./...
```
