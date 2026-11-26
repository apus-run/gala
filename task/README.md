# task

工作负载层：把「何时跑、谁来跑、怎么收尾」从业务代码里拆出来。

```
task/
├── cron/   周期任务：cron 表达式 + 可选分布式租约
└── job/    一次性任务：显式注册表 + CLI/HTTP 触发 + 可选单实例租约
```

依赖方向：`应用 → task`，`task` 自身只依赖标准库：

- `task` 模块只依赖标准库；不 import `components/dlock`，也不 import 根模块 `gala`。
- 分布式能力通过 `task.Locker`（`Lock` / `Unlock` / `Refresh`）注入，应用把
  `components/dlock` 的后端适配进来。
- 业务逻辑（任务内容、payload 语义、幂等键、重试策略、告警）留在应用里，不进入本层。

## 分布式 cron

```go
c := cron.New(
    cron.WithLockerProvider(
        func(ctx context.Context, jobID string) (task.Locker, error) {
            return lockClient.NewLock(ctx,
                dlock.WithLockName("order:cron:"+jobID),
                dlock.WithLockTimeout(30*time.Second),
            )
        },
        // Lease options are LockOption and belong inside WithLockerProvider.
        // 租约选项是 LockOption，必须放在 WithLockerProvider 内部。
        cron.WithLockRefreshInterval(10*time.Second),
    ),
)
c.MustAdd("daily-report", "@daily", runReport, cron.SkipIfRunning())
c.Start()
defer c.Shutdown(context.Background())
```

语义是**每任务 leader 租约**：所有副本都注册任务，第一个抢到该任务锁的副本成为 leader 并
执行；leader 在后台续租，续租失败即退位（不释放租约——它可能仍然健康），下一次触发点用
缓存的 `Locker` 重新抢锁。这样即使某个副本的 tick 延迟，也不会重复执行同一个触发点。

`LockerProvider` 在任务首次到期时才惰性调用；返回 `(nil, nil)` 表示该任务只在本地运行、
不参与分布式协调，且之后不再调用 provider；返回错误则跳过本次触发并上报
`LockProviderFailed`，下次到期重试。锁相关错误通过 `WithLockEventHandler` 上报，包本身
不 import dlock，因此不区分 `ErrLocked` 与其它错误——由应用用 `errors.Is` 过滤。

停止与移除的收尾语义：

- **Shutdown**：只停 ticker，再由唯一的共享后台等待者排空在途任务并释放全部租约。等待者
  即使 `ctx` 到期也继续运行，所以超时的 `Shutdown` 只返回 `ctx.Err()`，租约仍会在任务
  结束后释放，无需再次调用；重复或并发的 `Shutdown` 共享同一个等待者。
- **Stop**：立即释放租约、不等在途任务，因此另一副本可能接管；分发后尚未通过租约闸门的
  触发会被隔离，不再获取。`Stop`/`Shutdown` 的所有释放共享一份 acquire 超时预算（默认
  5s）并并发执行，N 个租约不会花费 N 份超时。
- **Remove / RemoveAll**：在 Cron 锁下写墓碑后异步释放，调用立即返回。`Unlock` 失败会保留
  墓碑与缓存的 `Locker`，由之后的 `Stop`/`Shutdown` 重试；重试成功前，重新添加的同名 id
  仍被墓碑隔离。
- **leader 代数**：每次成功 `Lock` 递增 leader 代数，释放执行前与快照比对。若期间已有更新
  的 leadership（例如并发 `Start` 之后重新获取），旧释放不会取消新续期器、不会 `Unlock`
  新租约，也不会重新写墓碑。

## 一次性 job

```go
registry := job.NewRegistry()
_ = registry.Register("rebuild-index", func(ctx context.Context) error {
    inv, _ := job.FromContext(ctx)
    return rebuild(ctx, inv.Payload)
})

// CLI: app --job=rebuild-index --job-data='{"shard":1}'
if err := registry.RunCLI(ctx, os.Args[1:], os.Stdin); err != nil { ... }

// HTTP: POST /jobs, X-Gala-Job-Name: rebuild-index
mux.Handle("/jobs", registry.Handler())
```

可选的 `job.WithLockerProvider(...)` 在每次 Run 时解析一个**独立的** `task.Locker`
（自带本副本的令牌，dlock 的 `Client.NewLock` 即如此），运行前抢租约、运行中续租。
provider 可按任务名选择锁名（例如 `"job:"+name`），不同任务互不阻塞；返回 `(nil, nil)`
表示本次不加租约，返回错误则本次不运行任务。`Lock` 失败时 `Run` 以 `%w` 包装错误返回，
`errors.Is(err, dlock.ErrLocked)` 仍然可用。

续期失败（不是收尾时的主动中止）会记录续期错误、取消 job 的派生 context（行为良好的
任务据此停止）并停止续期；job 返回后，仍在途的续期会被立即中止，而不是等满 acquire
超时，该中止不算错误。随后 `Run` 用携带调用方值、但不继承其取消的 context 释放租约，
因此调用方断开仍能解锁；任务、续期与解锁错误以 `errors.Join` 一起返回。默认续期间隔
5s、每次租约操作超时 5s，可用 `job.WithLockRefreshInterval` 与
`job.WithLockAcquireTimeout` 调整。

HTTP 适配器默认把请求体上限设为 10 MiB（`defaultMaxBodyBytes`），超过上限返回 413 且
任务不运行，`job.WithMaxBodyBytes(n)` 可调整上限（非正值保留默认）；未知任务名在读取
请求体之前就返回 404。任务运行错误默认统一映射为通用 500（不泄露细节，但也拿不到错误
内容），生产环境应安装 `job.WithHTTPErrorHandler` 记录完整错误（含 `*job.PanicError`
堆栈），并把 `dlock.ErrLocked` 等租约竞争映射为 409/503，便于客户端区分可重试与
永久失败。

## 接入应用生命周期

`gala` 根模块提供 `Task` 生命周期接口（`Start(ctx) error` / `Stop(ctx) error`）与
`WithTasks`。`task/cron` 通过 `cron.NewTask` 适配：

```go
app := gala.New(
    gala.WithServers(httpServer),
    gala.WithTasks(cron.NewTask(c)), // Start 启动调度；Stop 排空在途任务并释放租约
    gala.WithStopTimeout(20*time.Second),     // 服务器排水上限，也作为任务的回退预算
    gala.WithTaskStopTimeout(15*time.Second), // 可选：单独限定每个后台任务
)
if err := app.Run(); err != nil { ... }
```

任务在服务器启动、服务注册（若配置）与 `afterStart` 钩子之后按选项顺序启动；任一
`Start` 失败会跳过剩余任务，并在全部任务停止后返回该错误。

停机顺序是「先停 task（不再产生新任务）→ 再停 transport server」：服务器的停止会等待
所有 task 的 `Stop` 完成，因此任务在收尾时仍可使用数据库/HTTP 客户端。每个 task 的
`Stop` 都获得独立的新鲜 context：携带应用 context 的值，但不继承其取消信号；预算优先
取 `WithTaskStopTimeout`，其次 `WithStopTimeout`，两者都未设置时默认 30s。因此单个任务
耗尽预算不会饿死其后停止的任务，外部取消应用 context 也不会截断清理；task 的 `Stop`
错误会被收集并 join 进 `Run` 的返回值。若某个 task 的停止预算耗尽，它的 `Stop` 返回
`ctx.Err()`（会出现在 `Run` 的返回值里）；`cron` 的共享等待者仍在后台排空并释放租约，
无需再次调用。

## 从 components/cron 迁移

cron 引擎从 `github.com/apus-run/gala/components/cron` 迁到
`github.com/apus-run/gala/task/cron`。

> **破坏性变更（breaking）**：`components/cron` 已退役，不再接收功能更新，只保留
> 已发布的 tag（frozen tags）供旧代码继续构建。`task` 是**独立 Go module**，拥有
> 自己的 tag 版本线：`components/cron` 的版本号不会延续到 `task`，`task/vX.Y.Z`
> 的 X.Y.Z 与该模块自己的提交历史对应。不要假设 `task@v<旧 components/cron 版本>`
> 存在，请用 `go list -m -versions github.com/apus-run/gala/task` 查询已发布版本。
>
> **滚动升级风险**：旧 `components/cron` 副本不做任何租约协调，每个副本都会在本地
> 触发所有任务；新 `task/cron` 副本按任务抢 leader 租约。混跑期间同一个任务会在
> 每个旧副本上各执行一次，**外加**新 leader 的一次，非幂等任务会重复执行。上线
> 时应先把 `components/cron` 副本全部下线（或让旧副本不注册任务），再让
> `task/cron` 副本接管；不要长时间混跑。

| 变更 | 旧 | 新 | 动作 |
| --- | --- | --- | --- |
| import 路径 | `components/cron` | `task/cron` | 替换 import；包名不变 |
| module | 独立 module，已冻结 | 新独立 module，独立 tag 线 | `go get github.com/apus-run/gala/task@latest`（版本号不沿用） |
| 进程内调度 | 相同 API | 相同 API | 无需改代码 |
| 分布式 | 不支持 | `WithLockerProvider` 可选开启 | 需要分布式时接线 dlock |
| 混跑 | — | 不与旧副本协调 | 先下线旧副本再切新副本，避免重复执行 |

## 已知边界

- **不是 exactly-once**：租约只保证同一时刻至多一个副本在调度；切主窗口（最多一个 TTL）
  内可能错过触发点，不做补跑。
- **没有 fencing token**：长时间 STW/GC 后旧 leader 仍可能在租约过期后继续写共享资源
  （与 `components/dlock` 的已知边界一致）。
- **运行中的 cron 任务不会被取消**：`cron` 的 job 函数不接收 context；租约丢失后只停止
  后续调度，已在跑的任务继续。`Stop` 不等在途任务就释放租约，另一副本可能接管；超时的
  `Shutdown` 返回后，剩余任务仍在后台运行，租约在其结束后才释放。需要可取消语义的任务
  应自行把工作拆到可取消的 goroutine，或等待后续的 context-aware 任务 API。
- **移除后的释放是异步的**：`Remove`/`RemoveAll` 立即返回；`Unlock` 失败或超出预算时，
  墓碑、租约状态与缓存的 `Locker` 保留到之后的 `Stop`/`Shutdown` 重试成功，期间重新添加
  的同名 id 不会获得租约。
- **job 的 HTTP 适配器是最小实现**：只有针对整个 Handler 的 10 MiB 请求体上限
  （`WithMaxBodyBytes`），无鉴权、无服务端超时、无按任务路由；默认 500 不泄露错误，也不
  保留服务端痕迹（需要日志请用 `WithHTTPErrorHandler`）。
- **没有运行历史与持久化**：任务结果、失败重试、misfire 补偿不属于本层。

## 验证

```sh
cd task
go test -race ./...
```
