# layercache

`layercache` 按 **本地 L1 → 远端 L2 → 数据源** 组织缓存，提供进程内请求合并、条件写入、批量加载、负缓存和后台刷新。默认只有一个容量受限的本地缓存；传入 Remote 后组成两级缓存。

它适合数据库查询结果等可序列化数据。同一实例会阻止失效前启动的旧任务重新发布结果；跨实例更新仍依赖应用接入失效事件和 TTL，不提供数据库事务一致性。

模块路径：`github.com/apus-run/gala/components/layercache`。当前 [go.mod](go.mod) 要求 Go 1.27。

- [快速开始](#快速开始)
- [缓存层与 Redis](#缓存层与-redis)
- [读写与条件写](#读写与条件写)
- [批量读取与泛型 ID](#批量读取与泛型-id)
- [TTL 与负缓存](#ttl-与负缓存)
- [并发与一致性](#并发与一致性)
- [后台刷新](#后台刷新)
- [跨实例失效事件](#跨实例失效事件)
- [错误与可观测性](#错误与可观测性)
- [配置参考](#配置参考)
- [扩展后端与 Codec](#扩展后端与-codec)
- [生命周期与测试](#生命周期与测试)
- [兼容性调整](#兼容性调整)

## 快速开始

下面是一个不需要 Redis 的完整示例。实际项目中，把 `loadUser` 替换为数据库或服务查询，并使用传入的 context 执行 I/O。

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/apus-run/gala/components/layercache"
)

type User struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func loadUser(ctx context.Context, key string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if key != "user:42" {
		return User{}, layercache.ErrNotFound
	}
	return User{ID: 42, Name: "alice"}, nil
}

func main() {
	ctx := context.Background()
	base, err := layercache.New(
		layercache.WithNamespace("users:v2"),
		layercache.WithLocalCapacity(1000),
		layercache.WithLocalTTL(10*time.Second),
	)
	if err != nil {
		panic(err)
	}
	defer base.Close()

	users := layercache.NewTyped[User](base)
	user, err := users.GetOrLoad(ctx, "user:42", loadUser)
	if err != nil {
		panic(err)
	}
	fmt.Println(user.ID, user.Name) // 42 alice
}
```

三种入口共用同一个 Cache 的存储、请求合并、统计和生命周期：

| 入口 | 适用场景 | 示例 |
| --- | --- | --- |
| `Cache` | 显式传入读取目标，使用单次操作选项 | `base.Get(ctx, key, &user)`、`base.Once(...)` |
| `Typed[V]` | 字符串业务 key、固定值类型 | `users.GetOrLoad(ctx, key, loader)` |
| `T[K, V]` | 按前缀和有序 ID 访问 | `usersByID.GetOrLoad(ctx, "user", id, loader)` |

`NewTyped` / `NewT` 要求非 nil Cache，否则 panic。Cache 的零值不可用，也不要复制 Cache。多个类型视图可以共用一个实例，但不兼容的数据结构必须使用不同 key 域或 namespace。

下文的 API 片段沿用示例中的 `ctx`、`base`、`users`、`User` 和 `loadUser`；其他依赖会在对应场景中说明。更多可运行示例见 [example_test.go](example_test.go)。

## 缓存层与 Redis

| 模式 | 构造选项 | `CacheType()` |
| --- | --- | --- |
| 本地缓存 | 只传 `WithNamespace(...)` | `TypeLocal` / `"local"` |
| 两级缓存 | 添加 `WithRemote(remote)`，保留默认 L1 | `TypeBoth` / `"both"` |
| 仅远端 | `WithRemote(remote)` 加 `WithLocal(nil)` | `TypeRemote` / `"remote"` |

默认 L1 最多保存 10,000 条目，默认 codec 是标准库 JSON。`WithLocalCapacity(n)` 选择一个独立内置 L1；`WithLocal(local)` 选择自定义后端，传 nil 关闭 L1。两者按顺序覆盖，最后传入的生效。L1、L2 都关闭时，`New` 返回配置错误。

接入 Redis 时，应用负责创建、配置和关闭 go-redis 客户端。以下片段需导入 `github.com/apus-run/gala/components/layercache/redisstore` 和 `github.com/redis/go-redis/v9`：

```go
rdb := redis.NewClient(&redis.Options{
	Addr:                  "127.0.0.1:6379",
	ContextTimeoutEnabled: true,
	DialTimeout:           time.Second,
	ReadTimeout:           500 * time.Millisecond,
	WriteTimeout:          500 * time.Millisecond,
	MaxRetries:            1,
})
defer rdb.Close()

remote, err := redisstore.New(rdb)
if err != nil {
	panic(err)
}
base, err := layercache.New(
	layercache.WithNamespace("users:v2"),
	layercache.WithRemote(remote),
	layercache.WithLocalTTL(5*time.Second),
	layercache.WithRemoteTTL(5*time.Minute),
)
if err != nil {
	panic(err)
}
defer base.Close() // 先停止 Cache，再由应用关闭 rdb
```

`redisstore.New` 只校验客户端，不主动建立连接，也不修改客户端选项。适配器支持 go-redis 的 Client、ClusterClient 和 Ring；自定义客户端至少实现 `redisstore.Client` 的 `Process` 方法。

- 客户端应开启 `ContextTimeoutEnabled`，并配置合适的网络超时和重试次数；`WithRemoteTimeout` 不能替代客户端本身的超时设置。
- Redis 读取通过单 key Lua 脚本获取值与 PTTL 的原子快照。批量能力使用逐 key pipeline，支持跨 slot，不是多 key 事务。
- Redis TTL 按毫秒处理，不足 1ms 的写入会失败。返回的本地截止时间会扣除请求耗时和精度余量；极短 TTL 可能成功写入 L2，但没有足够剩余寿命回填 L1。
- 自定义客户端包装器必须保留命令的 `NoRetry` 属性。条件写和租约的模糊网络错误会直接返回，避免重试把“结果未知”误报成“条件拒绝”。

## 读写与条件写

### Get、Once 与 GetOrLoad

```go
var user User
err := base.Get(ctx, "user:42", &user) // 只读缓存，不调用数据源

err = base.Once(ctx, "user:42",
	layercache.Value(&user),
	layercache.Do(func(task context.Context) (any, error) {
		return loadUser(task, "user:42")
	}),
	layercache.TTL(time.Minute),
)

user, err = users.GetOrLoad(ctx, "user:42", loadUser)
```

读取顺序如下：

```text
Get:       L1 → L2 → ErrMiss
Once:      L1 → 合并请求并复查 L1/L2 → Do → 序列化 → 写入缓存
GetOrLoad: Typed 对 Once 的类型适配
```

- `Get` 的非 nil 目标必须是非 nil 指针。`Get(ctx, key, nil)` 只验证条目包，不解码业务 payload。
- `Once` 的 `Value` 是输出指针；传 nil 或不传可用于预热。未提供 `Do` 时只查缓存，miss 返回 `ErrMiss`。
- `Typed.GetOrLoad` 的 Loader 不能为 nil；只读缓存请使用 `Typed.Get`。
- `GetSkippingLocal` 只读 L2，不读取或回填 L1；没有 L2 时返回 miss。
- `Exists` 对 miss 和负缓存返回 `false, nil`，其他错误继续返回。它不完整解码业务 payload，不能代替一次带目标的 `Get` 来检查业务数据是否可解码。

### Set、Delete 与本地失效

```go
err := users.Set(ctx, "user:42", User{ID: 42, Name: "alice"},
	layercache.TTL(time.Minute),
)
err = base.Set(ctx, "user:42", layercache.Value(User{ID: 42, Name: "alice"}))
err = users.Delete(ctx, "user:42")
err = users.InvalidateLocal(ctx, "user:42")
```

配置 L2 后，写入先提交 L2，再发布 L1；L2 写入失败不会把候选新值留在 L1。显式 Set 和 Delete 会使当前实例中更早启动的同 key 共享任务失效，防止旧结果重新回填。

`Delete` 清理本地并删除远端；`InvalidateLocal` 只清理本地和阻止旧任务回填，不删除 L2，也不取消已有的后续刷新登记。L1 自然淘汰不会删除 L2。

原始 `Cache.Set` 同时提供 `Value` 和 `Do` 时，`Do` 优先；`Value(nil)` 是合法输入。Typed 的显式值或 Loader 优先于 ItemOption 中的 `Value` / `Do`，因此 `Typed.Set` 不会执行选项里的 `Do`。

### NX、XX 与跳过 L1

```go
err := users.Set(ctx, "user:42", User{ID: 42, Name: "alice"},
	layercache.SetNX(true),
)
if errors.Is(err, layercache.ErrNotStored) {
	// 已有条目，候选值未写入。
} else if err != nil {
	panic(err)
}
```

纯本地模式在同 key 的协调范围内检查条件；存在 L2 时，条件由 `ConditionalRemote` 原子执行。Redis 适配器已实现该能力。NX / XX 判断的是缓存条目是否存在，不是数据库记录是否存在。

`SetNX(true)` 与 `SetXX(true)` 不能同时启用。显式 Set 被明确拒绝时返回 `ErrNotStored`，保留原条目、TTL 和刷新登记；网络错误可能意味着写入结果未知，不能当成条件拒绝。Once / GetOrLoad 只在实际写入时应用条件，普通自动回填被拒绝时仍可返回源值，并由 ErrorHandler 报告。

`SkipLocal(true)` 可用于 Set / Once / Typed.GetOrLoad，需要存在 Remote。它跳过 L1 读取和新值回填；提交写入时仍可能清理旧 L1。它与 `GetSkippingLocal` 的只读接口不同，不能在纯本地模式使用。

## 批量读取与泛型 ID

### Typed.MGet

```go
source := map[string]User{
	"user:42": {ID: 42, Name: "alice"},
	"user:43": {ID: 43, Name: "bob"},
}
values, err := users.MGet(ctx, []string{"user:43", "user:42", "user:43"},
	func(task context.Context, missing []string) (map[string]User, error) {
		// missing 已排序、去重，仅包含需要回源的 key；不要修改该切片。
		result := make(map[string]User, len(missing))
		for _, key := range missing {
			if value, ok := source[key]; ok {
				result[key] = value
			}
		}
		return result, nil
	},
)
if err != nil {
	// values 仍可能包含成功结果，按业务需要处理部分成功。
	fmt.Printf("partial result: %v, error: %v\n", values, err)
}
```

`MGet(ctx, keys, nil)` 只读缓存。结果 map 只包含成功读取或加载的正值；miss 和负缓存对应的 key 不在 map 中，不逐项返回 `ErrMiss` / `ErrNotFound`。

批量错误通过 `errors.Join` 汇总，可能与部分结果同时返回：

- Loader 返回 nil error 时，遗漏的 key 才被视为确实不存在；启用负缓存后可写入负条目。
- Loader 返回 error 时，已返回的成功值仍可使用和缓存，但遗漏项不会写成负缓存。
- 远端读失败的 key 不会被当作 miss 回源；其他已确认 miss 的 key 仍可继续处理。
- 多 key 写入不具备事务性，部分 L2 写入失败时，返回值可能仍含已从源取得的数据。
- 只有相同的标准化缺失 key 集合才共享批量任务；重叠但不同的集合不保证合并。

### T[K, V]

```go
usersByID := layercache.NewT[int64, User](base)
user, err := usersByID.GetOrLoad(ctx, "user", int64(42),
	func(task context.Context, id int64) (User, error) {
		return loadUser(task, fmt.Sprintf("user:%d", id))
	},
)
```

`K` 满足 `cmp.Ordered`。默认业务 key 为 `prefix + ":" + fmt.Sprint(id)`，分隔符可通过 `WithSeparator` 修改；前缀和 ID 不自动转义，应用需保证 key 不碰撞。

`T` 提供 Set、Get、GetOrLoad、GetSkippingLocal、Delete、InvalidateLocal、Exists 和 MGet。`Get(..., nil)` 只读缓存；`GetOrLoad(..., nil)` 是无效调用。访问原始字符串 key、统计或生命周期时使用 `usersByID.Typed`；所有门面共享底层 Cache，关闭一次即可。

## TTL 与负缓存

正值默认 TTL：纯本地模式使用 `LocalTTL`，存在 L2 时使用 `RemoteTTL`。单次 `TTL(d)` 覆盖正值期限，但不会突破 L1 的本地上限：

```text
L1 截止时间 = min(当前时间 + LocalTTL, 条目剩余截止时间)
实际写入 TTL = (正值 TTL 或 NotFoundTTL) + [0, TTLJitter)
```

因此，默认 LocalTTL 为 5s 时，即使传入 `TTL(time.Minute)`，L1 也最多保留 5s。远端命中回填会沿用剩余期限，不会把 L2 的 TTL 重新计满。所有条目必须有有限正 TTL，不支持永久缓存条目。

负缓存默认关闭。开启时显式设置 `WithNotFoundTTL`；如需识别数据库的缺失错误，再设置 `WithErrNotFound`：

```go
base, err := layercache.New(
	layercache.WithNamespace("users:v2"),
	layercache.WithNotFoundTTL(2*time.Second),
	layercache.WithErrNotFound(sql.ErrNoRows), // 需导入 database/sql
)
if err != nil {
	panic(err)
}
defer base.Close()
```

- `ErrNotFound` 始终被识别；`WithErrNotFound` 只增加一个通过 `errors.Is` 匹配的错误，不自动开启负缓存。
- 负缓存命中不会再次回源。配置自定义缺失错误后，负条目读取会返回该错误，统一判断可使用 `base.IsNotFound(err)`。
- 普通源错误不写入负缓存；零值、空字符串和 nil 的成功结果也不是“未找到”。
- `TTL(...)` 不覆盖负缓存 TTL；负条目使用 `NotFoundTTL`，再应用 jitter 和 L1 上限。

## 并发与一致性

`Once` / `GetOrLoad` 在同一 Cache 实例内合并同 key 的慢路径，包括 L2 读取、Loader 和回填。TTL、SkipLocal、条件模式以及是否提供 Loader 不同，会形成不同任务；同 key 的 Loader 必须有兼容的业务语义，不能依赖“哪个调用者先进入”。

等待者取消只结束自己的等待。共享任务保留首个调用者的 context values，脱离其取消和 deadline，由 `LoadTimeout` 与 `Close` 控制；即使所有等待者都离开，任务仍可能完成回填。Loader 应使用传入的任务 context，不要捕获外部请求 context 来替代它。

`WithMaxConcurrentLoads` 限制共享单键任务、批量任务和后台刷新任务的总数，不是等待者数量。普通 Get 与同步 `Set(..., Do(fn))` 不占该共享任务预算。不同 key 的后端 I/O 可以并行，同 key 发布按序协调。

Set、Delete、本地失效、刷新和批量发布会检查或推进实例内的 key 代次。旧任务若在失效后完成，其旧结果不会发布，等待者可收到 `ErrInvalidated`。这只保护当前实例内的协调状态，不阻止其他进程写入，不等同于分布式锁或数据库 fencing。

业务更新通常先提交数据库，再删除或更新缓存。没有接入跨实例失效事件时，其他实例的 L1 可能继续服务旧值，直到本地 TTL 到期。事件同样是尽力通知，因此不能用它承诺强一致性。

## 后台刷新

先配置全局刷新间隔，再为需要刷新的 key 提供 Loader 和 `Refresh(true)`：

```go
base, err := layercache.New(
	layercache.WithNamespace("users:v2"),
	layercache.WithRefreshInterval(10*time.Second),
	layercache.WithRefreshIdleTimeout(time.Minute),
	layercache.WithRefreshConcurrency(4),
)
if err != nil {
	panic(err)
}
defer base.Close()

users := layercache.NewTyped[User](base)
user, err := users.GetOrLoad(ctx, "user:42", loadUser, layercache.Refresh(true))
```

- `RefreshInterval = 0` 关闭刷新；开启时至少为 1s。`Refresh(true)` 必须同时有 Loader 和有效刷新间隔。
- 只有带 `Refresh(true)` 的访问续期刷新登记；普通 Get 不续期。空闲超过 `RefreshIdleTimeout` 后移除登记，`TaskSize()` 返回当前登记数。
- 同一 key 已登记时，再次登记只续期访问时间，继续使用原 Loader 和单次配置。更换定义可通过显式 Set / Delete 取消旧登记后重新注册。
- 成功的显式 Set 和 Delete 会取消旧刷新登记；条件写明确拒绝时保留登记。`InvalidateLocal` 保留后续刷新。
- 有 L2 时，Remote 必须实现 `LeaseRemote`。刷新持有者回源，未取得租约的实例可从 L2 同步 L1；租约协调的是后台刷新，不合并跨实例的首次 miss，也不提供数据库写入 fencing。
- 刷新结果为不存在且负缓存关闭时，会移除被替代的正条目。后台失败通过 ErrorHandler 观察。

刷新登记数、刷新并发数和共享任务总数分别受 `MaxRefreshTasks`、`RefreshConcurrency` 和 `MaxConcurrentLoads` 约束。`Set(..., Refresh(true))` 可能已经写入成功，但随后因刷新登记满而返回 `ErrOverloaded`；调用方不能把所有 error 都解释为“值没有写入”。

## 跨实例失效事件

`WithEventHandler` 启用本实例的变更事件回调，需要同时有 L1 和 L2。库只产生事件，不创建 Redis Pub/Sub、消息队列或订阅消费者；传输与接收由应用负责。

```go
// publish 由应用实现，签名为 func(context.Context, layercache.Event) error。
base, err := layercache.New(
	layercache.WithNamespace("users:v2"),
	layercache.WithRemote(remote),
	layercache.WithEventHandler(publish),
)
if err != nil {
	panic(err)
}
defer base.Close()
```

接收端可在独立的订阅处理流程中只失效自己的 L1：

```go
func applyEvent(ctx context.Context, cache *layercache.Cache, event layercache.Event) error {
	if event.Namespace != cache.Namespace() || event.SourceID == cache.SourceID() {
		return nil
	}
	for _, key := range event.Keys {
		if err := cache.InvalidateLocal(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
```

事件类型为 `EventSet`、`EventLoad`、`EventRefresh`、`EventBatch`、`EventDelete`；Keys 是未加 namespace 包装的业务 key。SourceID 默认逐实例生成；自定义 ID 必须实例间唯一。同一 namespace 的实例才应互相处理事件。

事件队列有界、异步、尽力投递。队列满会丢弃事件并增加 `DroppedEvents`；回调失败不会回滚已确认的缓存写入。`InvalidateLocal` 不发送新事件，可避免订阅端循环广播。事件不是可靠消息日志，应用仍应使用 TTL 限制陈旧数据的存活时间。

## 错误与可观测性

优先使用 `errors.Is` / `errors.As`，不要依赖错误字符串。批量操作尤其需要同时检查返回 map 和 error。

| 错误 | 含义 |
| --- | --- |
| `ErrMiss` | 缓存未命中；不等于源记录不存在 |
| `ErrNotFound` / 自定义缺失错误 | 数据源不存在或负缓存命中；可用 `Cache.IsNotFound` 统一判断 |
| `ErrCorrupt` | 条目版本、包格式或业务 payload 无法解析，不自动回源 |
| `ErrInvalidTTL` | TTL 非法、溢出或后端返回无效截止时间 |
| `ErrInvalidOptions` | namespace、目标指针、选项组合等配置错误 |
| `ErrUnsupported` | 缺少所需后端或 NX/XX、刷新租约能力 |
| `ErrNotStored` | 显式条件写被明确拒绝；自动回填的拒绝可通过 ErrorHandler 观察 |
| `ErrInvalidated` | 共享结果已被更晚的失效或写入取代 |
| `ErrOverloaded` | 共享任务或刷新登记预算已满 |
| `ErrClosed` | Cache 已关闭或其工作正在被关闭取消 |
| `context.Canceled` / `context.DeadlineExceeded` | 调用或任务预算终止 |

`OpError` 带有 `Op`、`Key` 和底层错误，Key 包含 namespace 编码，可通过 `Unwrap` 继续匹配；`PanicError` 记录共享任务或受保护后台回调中的 panic 值及堆栈。

远端读取失败、永久条目或数据损坏不会被当成 miss，GetOrLoad 不会因此自动打到数据源。普通自动回填失败时，GetOrLoad 可返回已加载的源值，并交给 `WithErrorHandler` 报告；失效、关闭和任务超时仍会返回错误。显式 Set / Delete 返回相应写入错误，且失败后旧 L1 也可能已经清理。

```go
stats := base.Stats()
fmt.Printf("local hits=%d remote hits=%d loads=%d dropped events=%d\n",
	stats.LocalHits, stats.RemoteHits, stats.Loads, stats.DroppedEvents)
```

`Stats()` 是累计原子计数快照，各字段不是同一事务中的一致采样；统计的是后端探测和源执行次数，不是唯一请求数或唯一 key 数。还可观察 `LocalMisses`、`RemoteMisses`、`LoadErrors`、`Refreshes` 和 `RefreshErrors`。配置 `WithStatsHandler` 后按 `WithStatsInterval` 周期发送快照；`WithStatsDisabled(true)` 停止计数及周期发送。

ErrorHandler 默认不打印日志。所有 Loader、codec、后端及回调闭包都应并发安全；回调须及时返回，不得同步重入同一 Cache 或调用它的 Close。

## 配置参考

### 构造选项

| Option | 默认值 / 约束 |
| --- | --- |
| `WithNamespace` | 必填、非空白；隔离业务 key 和刷新租约域 |
| `WithLocalCapacity` / `WithLocal` | 独立内置 L1、10,000 条目；自定义 Local 由应用管理，nil 关闭 L1 |
| `WithRemote` | 默认无 L2；nil 表示不使用远端 |
| `WithCodec` | 标准库 JSON；显式 nil 无效 |
| `WithLocalTTL` / `WithRemoteTTL` | 5s / 5min，均须为正 |
| `WithNotFoundTTL` | 0 关闭负缓存，正值开启 |
| `WithErrNotFound` | 默认只识别 `ErrNotFound`；不改变负缓存开关 |
| `WithTTLJitter` | 0；添加 `[0, jitter)`，不能为负或使 TTL 溢出 |
| `WithLoadTimeout` / `WithRemoteTimeout` | 2s / 250ms，均须为正 |
| `WithMaxConcurrentLoads` | 128，须为正；共享任务总预算 |
| `WithErrorHandler` | nil；自动回填和后台失败观察回调 |
| `WithRefreshInterval` | 0 关闭；开启时至少 1s，有 L2 时要求 LeaseRemote |
| `WithRefreshIdleTimeout` | 0 表示由 New 派生为刷新间隔 + 1s；不能为负 |
| `WithRefreshConcurrency` / `WithMaxRefreshTasks` | 4 / 10,000，均须为正 |
| `WithEventHandler` | nil 关闭；非 nil 要求同时配置 L1、L2 |
| `WithSourceID` / `WithEventBuffer` | New 逐实例生成 ID / 100；缓冲大小须为正 |
| `WithStatsDisabled` | false |
| `WithStatsHandler` / `WithStatsInterval` | nil / 1min；间隔须为正 |
| `WithSeparator` | `:`，供 T 门面拼接前缀和 ID |

普通默认值来自 `DefaultOptions()`，每次返回独立配置。SourceID 和自动刷新空闲期限由 `New` 逐实例生成或派生。配置校验只看最终值，同一项后传覆盖先传；显式传入错误值不会被替换成默认值。

`Options` 字段私有。`NewOptions` / `Options.Apply` 只按序组装，不校验、不回滚、不创建内置后端；`Validate` 只检查最终配置；`New` 校验后保存配置值副本，再创建独立内置 L1 和后台任务。通过 `OptionFunc` 组合可复用配置：

```go
profile := layercache.OptionFunc(func(o *layercache.Options) {
	o.Apply(
		layercache.WithLocalCapacity(1000),
		layercache.WithLocalTTL(10*time.Second),
	)
})
base, err := layercache.New(layercache.WithNamespace("users:v2"), profile)
if err != nil {
	panic(err)
}
defer base.Close()
```

复用 Option 不会共享内置 L1。自定义 Local 只能交给一个 Cache；传入的后端、codec 和闭包依赖仍共享应用对象，不会被深拷贝。

### 单次操作选项

| ItemOption | 语义 |
| --- | --- |
| `Value(v)` | Set 的输入值；Once 的输出指针，nil 可用于预热 |
| `Do(fn)` | 数据源函数；Once 的 nil Do 表示只查缓存 |
| `TTL(d)` | 覆盖本次正值期限，必须大于零 |
| `SetNX(true)` / `SetXX(true)` | 原子条件写；不能同时启用 |
| `SkipLocal(true)` | 跳过 L1 读取和回填，需要 Remote |
| `Refresh(true)` | 登记后台刷新，需要 Do 和有效刷新间隔 |

每次调用先取当前 Cache 的默认 TTL，再按序应用选项，只校验最终值。`ItemOptions` 字段私有，通过 `ItemOptionFunc` 和 `ItemOptions.Apply` 组合：

```go
insert := layercache.ItemOptionFunc(func(o *layercache.ItemOptions) {
	o.Apply(layercache.TTL(time.Minute), layercache.SetNX(true))
})
err := users.Set(ctx, "user:42", User{ID: 42, Name: "alice"}, insert)
```

Typed 的显式值、Loader 和目标优先于单次选项中的 `Value` / `Do`；复用 `[]ItemOption` 时，泛型门面不会改写调用方的底层切片。构造 Option 与单次 ItemOption 分开使用，不能混用。

`New` 拒绝 nil Option，Set / Once 拒绝 nil ItemOption，且在后端操作、回源和刷新登记前返回 `ErrInvalidOptions`。`WithLocal(nil)`、`WithEventHandler(nil)`、`Do(nil)`、`Value(nil)` 是合法选项，nil 是它们的业务值。直接调用 Apply 应只传有效选项；适配器不得保留配置用于后续修改或并发使用。

## 扩展后端与 Codec

接口定义见 [types.go](types.go)：

| 接口 | 要求与用途 |
| --- | --- |
| `Local` | 同步、并发安全的 Get / Set / Remove；保存 Entry 的真实截止时间；一个实例只交给一个 Cache |
| `Remote` | 实现 Get / Set / Delete，遵守 context；读返回值与有效期限的原子快照，写返回保守的非零截止时间 |
| `ConditionalRemote` | 可选 NX/XX 能力；拒绝不能改变已有值或 TTL |
| `BatchRemote` | 可选 MGet / MSet；允许部分成功，不隐含跨 key 事务；缺失能力时退回逐 key 操作 |
| `LeaseRemote` | 可选刷新租约能力；原子获取空租约或续租同一 owner，不能抢占其他有效 owner |
| `Codec` | 并发安全的 Marshal / Unmarshal；不得复用或修改已交出的编码结果，不得修改解码输入 |

Local 的容量淘汰不应删除 L2。Remote 返回的 `Entry.Data` 是内部条目包，不是裸业务 JSON；自定义后端只保存和返回这些字节，不自行解释或转换包格式。

内置可选 codec 在构造时传入。例如导入 `msgpackcodec "github.com/apus-run/gala/components/layercache/codec/msgpack"` 后：

```go
base, err := layercache.New(
	layercache.WithNamespace("users:msgpack:v1"),
	layercache.WithCodec(msgpackcodec.Codec{MaxDecodedBytes: 16 << 20}),
)
if err != nil {
	panic(err)
}
defer base.Close()
```

- [codec/sonic](codec/sonic/codec.go)：Sonic JSON，传入该包的 `Codec{}`。
- [codec/msgpack](codec/msgpack/codec.go)：MessagePack；编码结果小于 64 字节时不压缩，否则使用 S2。`MaxDecodedBytes = 0` 使用 16 MiB 默认上限，负值无效。

`string`、`[]byte` 和 nil 使用内置快路径，不经过 Codec；MessagePack 的压缩及大小限制不覆盖这些快路径。写入期间不要并发修改入参；写入返回后再修改入参，不会改变已保存快照。内置 codec 每次读取都解码独立结果。

切换不兼容的 codec 或数据结构时使用新 namespace。不要用其他缓存库或业务 Redis 命令向同一 key 写入裸 payload，否则读取可能返回 `ErrCorrupt`。

## 生命周期与测试

`Close()` 幂等，会取消并等待当前操作、共享任务、刷新及观察者退出；关闭后新操作返回 `ErrClosed`。Loader、Remote 和回调必须响应 context 或及时返回，否则 Close 仍会等待。

Cache 不关闭应用持有的 Local、Remote 或 Redis 客户端。多个 Typed / T 门面共享同一 Cache 时，由拥有该 Cache 的组件统一关闭，随后再关闭后端连接。

从仓库根目录进入本模块运行：

```bash
cd components/layercache
go test -race ./...
go vet ./...
go test -run '^Example' -v .
```

默认单元测试使用内存后端 / miniredis，不要求真实 Redis。未配置以下环境变量时，对应真实集成测试会跳过：

```bash
# 已运行的独立 Redis；测试使用独立 namespace，不 FLUSHDB。
LAYERCACHE_REDIS_ADDR=127.0.0.1:6379 \
    go test -race -count=1 -run '^TestRedisIntegration$' ./redisstore

# 测试预期 3 个主节点、完整的 16384 个 slot，节点公布的地址需可访问。
LAYERCACHE_REDIS_CLUSTER_ADDRS=127.0.0.1:7000,127.0.0.1:7001,127.0.0.1:7002 \
    go test -race -count=1 -run '^TestRedisClusterIntegration$' ./redisstore
```

上述集成测试分别覆盖 RESP2 / RESP3。故障注入测试还需要 `LAYERCACHE_CLUSTER_FAULT_PROJECT` 指定带专用标签的独占 Docker 测试集群，会操作节点和网络；启用前按 [cluster_fault_test.go](redisstore/cluster_fault_test.go) 中的 fixture 约束准备环境。

可选的模糊测试：

```bash
go test . -run '^$' -fuzz '^FuzzDecode$' -fuzztime=10s
go test ./codec/msgpack -run '^$' -fuzz '^FuzzUnmarshal$' -fuzztime=10s
```

## 兼容性调整

从旧接口迁移时注意：

- 默认提供有容量上限的内置 L1。原本只配置 Remote、希望保持远端单层的调用，需要补 `WithLocal(nil)`。
- `Option` / `ItemOption` 为接口；自定义组合使用 `OptionFunc` / `ItemOptionFunc`，通过公开 Apply 组装私有配置。
- 使用 `WithNamespace`，移除同义入口 `WithName`；原 `WithStopRefreshAfterLastAccess` 改为 `WithRefreshIdleTimeout`。
- 移除 `WithSyncLocal`，事件只由 `WithEventHandler` 启停。
- `WithErrNotFound` 不再隐式开启负缓存；需要显式配置 `WithNotFoundTTL`。
