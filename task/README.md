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
执行；leader 在后台续租，续租失败即退位，下一次触发点重新抢锁。这样即使某个副本的 tick
延迟，也不会重复执行同一个触发点。

`LockerProvider` 返回 `(nil, nil)` 表示该任务只在本地运行、不参与分布式协调；返回错误则
跳过本次触发并上报 `LockProviderFailed`。锁相关错误通过 `WithLockEventHandler` 上报，
包本身不 import dlock，因此不区分 `ErrLocked` 与其它错误——由应用用 `errors.Is` 过滤。

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
（自带本副本的令牌，dlock 的 `Client.NewLock` 即如此），运行前抢租约、运行中续租；
续租失败会取消 job 的 context，并把续租错误与任务错误一起返回。provider 也可按任务名
选择锁名（例如 `"job:"+name`），不同任务互不阻塞；返回 `(nil, nil)` 表示本次不加租约。

HTTP 适配器默认把任务错误统一映射为通用 500（不泄露细节，但也拿不到错误内容），
生产环境应安装 `job.WithHTTPErrorHandler` 记录完整错误（含 `*job.PanicError` 堆栈），
并把 `dlock.ErrLocked` 等租约竞争映射为 409/503，便于客户端区分可重试与永久失败。

## 接入应用生命周期

`gala` 根模块提供 `Task` 生命周期接口（`Start(ctx) error` / `Stop(ctx) error`）与
`WithTasks`。`task/cron` 通过 `cron.NewTask` 适配：

```go
app := gala.New(
    gala.WithServers(httpServer),
    gala.WithTasks(cron.NewTask(c)), // Start 启动调度，Stop 排空并释放租约
)
if err := app.Run(); err != nil { ... }
```

停止顺序是「先停 task（不再产生新任务）→ 再停 transport server」，由应用的
`WithStopTimeout` 统一限时。

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
- **运行中的任务不会被取消**：`cron` 的 job 函数不接收 context；租约丢失后只停止后续
  调度，已在跑的任务继续。需要可取消语义的任务应自行把工作拆到可取消的 goroutine，或
  等待后续的 context-aware 任务 API。
- **没有运行历史与持久化**：任务结果、失败重试、misfire 补偿不属于本层。

## 验证

```sh
cd task
go test -race ./...
```
