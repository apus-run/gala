# 分布式锁

统一的 `Client` / `Locker` 契约与多后端实现。锁不自动续期、不启动后台协程；持有者必须在
`LockTimeout` 到期前显式 `Refresh`，并在 `Refresh` 返回 `dlock.ErrLockNotHeld` 时立即停止
操作被保护的资源。

## 安装

```sh
go get github.com/apus-run/gala/components/dlock
```

要求 **Go 1.27 或更高版本**。使用尚未发布的变更时，在应用的 `go.mod` 中通过 `replace`
指向本地 `components/dlock` 模块。

## 后端

| 后端 | 依赖 | 排他机制 | 释放 / 续期 | TTL 精度 | 首次使用 |
| --- | --- | --- | --- | --- | --- |
| `redis` | Redis | Lua 原子写入；Unlock/Refresh 比对令牌 | Lua 比对令牌后 DEL / PEXPIRE | 毫秒（PX） | 直接可用 |
| `etcd` | etcd | 租约 + 事务 `CreateRevision(key) == 0` | 撤销 / KeepAliveOnce 自己的 lease，并校验 key | 秒（向上取整） | 直接可用 |
| `consul` | Consul | session + `KV.Acquire` | `KV.Release` + 销毁 / Renew 自己的 session，并校验 key | 秒（10s–24h） | 直接可用 |
| `gorm` | MySQL、PostgreSQL | `name` 唯一索引 + `version` 乐观锁接管 | 按令牌删除 / 按令牌续写，均要求未过期 | 毫秒（应用时钟） | `gorm.Init(ctx, db)` |
| `mongo` | MongoDB | `name` 唯一索引 + 按名称 upsert 和条件更新；无索引时 Lock 失败（fail closed） | 按令牌删除 / 按令牌续写，均要求未过期 | 毫秒（应用时钟） | `mongo.Init(ctx, coll)`（缺失时 Lock 报错） |
| `noop` | 无 | 无排他性，永远成功 | 无 | 无 | 直接可用 |

`gorm` 与 `mongo` 的建表/建索引放在显式的 `Init(ctx)` 中，构造函数不做 I/O。

## 接口

```go
type Client interface {
    NewLock(ctx context.Context, opts ...Option) (Locker, error)
}

type Locker interface {
    Lock(ctx context.Context) error
    Unlock(ctx context.Context) error
    Refresh(ctx context.Context) error
}
```

- `WithLockName(name)`：锁名，默认 `gala-distributed-lock`。
- `WithLockTimeout(d)`：单次持有的 TTL，默认 `10s`，最小 `1ms`；`consul` 要求
  `10s ≤ d ≤ 24h`。
- `WithOperationTimeout(d)`：单次 `Lock`/`Refresh` 的上限（含全部存储往返），默认
  `5s`；建议小于 `LockTimeout`，让慢调用在耗尽租约前失败。
- `WithOwnerID(id)`：仅用于错误信息与诊断，默认主机名。

## 行为约定

- **不会自动续期。** 长任务自行启动刷新循环；忘记刷新时锁会在 `LockTimeout` 后过期，
  其他持有者可以接管。
- **所有权由随机令牌保证，而不是 `OwnerID`。** 每个 `Locker` 创建时生成
  `<ownerID>@<随机值>`；`redis`/`gorm`/`mongo` 在 `Unlock`/`Refresh` 时比对令牌，
  `etcd`/`consul` 依赖租约或 session 身份并在 `Refresh` 时额外校验 key。旧持有者在租约
  过期后既不能释放也不能续期新持有者的锁。
- **错误可判定。** `Lock` 在他人持有时返回 `dlock.ErrLocked`；`Unlock` / `Refresh` 在本
  Locker 已失去锁时返回 `dlock.ErrLockNotHeld`，均可用 `errors.Is` 判断。过期但尚未被
  接管时，五个真实后端的 `Unlock` / `Refresh` 都返回 `ErrLockNotHeld`。GORM 和 MongoDB
  在 `Lock` / `Refresh` 收到写入确认时核对已写入的到期时间，租约已过期会返回同一错误。
- **重复 `Lock` 幂等。** 同一 `Locker` 在持有期间再次 `Lock` 会延长 TTL 并返回成功。
- **不内建等待重试。** `Lock` 冲突时立即返回 `ErrLocked`；需要排队的调用方自行做退避重试。
- **`Unlock` 忽略调用方取消。** 释放属于清理动作：`ctx` 已取消时仍会带 `5s` 超时尝试释放，
  避免 `defer locker.Unlock(ctx)` 在请求取消后把锁留到 TTL。
- **`Lock`/`Refresh` 有操作上限。** 两者在调用方 `ctx` 之外再套 `OperationTimeout`（默认
  `5s`，取两者中更早的截止时间），存储卡住时返回超时错误而不是永久挂起。Redis 需要客户
  端开启 `ContextTimeoutEnabled`，否则 go-redis 以自身的 `ReadTimeout`/`WriteTimeout` 为准。
  Consul 和 etcd 的失败清理使用本次操作剩余的时间，未完成的 session 或 lease 由服务端 TTL 回收。
- **释放失败交给 TTL 兜底。** `Unlock` 返回错误时不要假设锁已经释放，等待 TTL 过期即可。

## 示例

```go
client := redis.NewClient(rdb)
locker, err := client.NewLock(ctx,
    dlock.WithLockName("orders:export"),
    dlock.WithLockTimeout(10*time.Second),
)
if err != nil {
    return err
}

if err := locker.Lock(ctx); err != nil {
    if errors.Is(err, dlock.ErrLocked) {
        return err // 其他实例正在执行
    }
    return err
}
defer locker.Unlock(ctx)

// 长任务：每 3s 续期一次，直到任务结束。
ticker := time.NewTicker(3 * time.Second)
defer ticker.Stop()
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-ticker.C:
        if err := locker.Refresh(ctx); err != nil {
            if errors.Is(err, dlock.ErrLockNotHeld) {
                return err // 锁已被接管，必须停止工作
            }
            return err
        }
    case <-done:
        return nil
    }
}
```

需要建表或建索引的后端，启动时先初始化一次：

```go
if err := gorm.Init(ctx, db); err != nil {      // 或 mongo.Init(ctx, collection)
    return err
}
client := gorm.NewClient(db)
```

etcd 的 Locker 持有内部 lease client，用完要 `Close`（不撤销仍持有的租约，所以先 `Unlock`）：

```go
locker, err := etcd.NewLock(ctx, etcdCli, dlock.WithLockName("export"))
if err != nil {
    return err
}
defer locker.Close()

if err := locker.Lock(ctx); err != nil {
    return err
}
defer locker.Unlock(ctx)
```

通过 `dlock.Client` 拿到 Locker 时，用 `io.Closer` 断言探测是否可关闭。`Close` 之后的
`Lock`/`Refresh`/`Unlock` 返回 `dlock.ErrClosed`。

## 从旧版本迁移

| 变更 | 旧行为 | 新行为 | 迁移动作 |
| --- | --- | --- | --- |
| 续期 | 部分后端启动后台 goroutine 自动续期（停止信号不完整，可能泄漏或竞态） | 不启动任何后台协程，必须显式 `Refresh` | 长任务自行启动刷新循环；不刷新会在 TTL 后失去锁 |
| 所有权 | 主机名作为锁值；Redis 直接 `DEL`/`EXPIRE`；GORM/Mongo 按 `name` 删除；etcd/Consul 直接覆盖 | 每个 Locker 随机令牌；释放/续期均带所有权条件 | **严禁新旧版本混跑**：旧实例的 `DEL`/`Delete`/`Put` 会破坏新锁。升级前清空锁键/锁表，或改用新锁名 |
| etcd 排他 | 无条件 `Put`，可覆盖他人的锁 | 事务 `CreateRevision == 0` 抢锁 | 同上；升级后旧实例必须停止 |
| Consul 排他 | 无条件 `Put`，可覆盖他人的锁 | `KV.Acquire` / `KV.Release` | 同上 |
| 错误语义 | 字符串错误 | `ErrLocked` / `ErrLockNotHeld` 哨兵错误 | 用 `errors.Is` 判断；不要依赖错误文本 |
| API | `redis.NewLock` / `etcd.NewLock` 只返回 `*Lock`；`noop.NewLock` 只返回 `*NoopLocke` | 均增加 `error` 返回（`noop` 为 `(*NoopLocke, error)`）；`redis.NewClient` 接受 `redis.Cmdable` | 处理构造错误；传入 `*redis.Client` 的代码无需修改 |
| GORM 建表 | `NewLock` 内部执行 `AutoMigrate` | 构造不再做 I/O，改为显式 `gorm.Init(ctx, db)` | 启动时调用一次 `Init`，否则首次 `Lock` 会因缺表/缺列失败 |
| GORM 表结构 | `gorm_locks` 只有 name/owner_id/expired_at 等列 | 新增 `token`、`version` 列与 `expired_at` 索引；`owner_id` 保持原类型（不限长） | `Init` 自动迁移，并把旧行的 `version IS NULL` 回填为 `0`、`token IS NULL` 回填为空串（否则 MySQL/PostgreSQL 读取时报错）。旧行令牌为空，只会在过期后被接管；建议升级前清空锁表 |
| Mongo 建索引 | `NewLock` **不会**创建唯一索引（旧版本也没有；没有索引时多个副本可同时加锁成功，互斥性静默失效） | 新增 `mongo.Init(ctx, coll)` 创建唯一索引；未调用时首次 `Lock` fail closed 并报错指明 `Init` | 启动时调用一次 `Init`；需要 `createIndexes` 权限，并先清理重复文档、删除同名非唯一索引。升级前必须补齐 `Init`，否则任务会直接失败而不是重复执行 |
| Consul TTL | 任意值，服务端可能拒绝 | `10s ≤ LockTimeout ≤ 24h`，否则构造时报错 | 调整配置 |

## 验证

在 `components/dlock` 目录下运行：

```sh
./test-unit.sh
./integration/run.sh
```

`test-unit.sh` 使用 `-race` 运行单元测试，按整个模块统计 Go 语句覆盖率，低于 99% 时返回失败。
测试使用 SQLite、miniredis 和驱动模拟响应，覆盖存储错误、租约丢失、并发等待和清理超时。

`integration/run.sh` 使用 Docker Compose 启动 Redis、MySQL、PostgreSQL、MongoDB、etcd 和 Consul。
六种配置执行同一套契约测试，包括并发抢锁、续期、自然过期接管和取消请求后的释放。
etcd 还验证同值 key 换绑租约，MongoDB 还验证索引移除与恢复、旧 ObjectID 接管、慢写入和过期清理。
测试容器使用本机随机端口和临时数据目录，脚本在退出时清理本次 Compose 项目。

单独验证 MongoDB 时，可将 `DLOCK_TEST_MONGODB_URI` 指向测试实例后运行
`go test -race -count=1 -run TestMongoLeases ./mongo`。该测试在 `gala_dlock_test` 库中创建独立集合，结束后删除。

## 已知边界

- **没有 fencing token。** TTL 锁无法阻止长时间 STW/GC 后的旧持有者继续写共享资源。受保护
  资源需要自行校验单调递增的版本/租约，或在进入临界区前再次 `Refresh` 并确认仍持有锁。
- `gorm` / `mongo` 用应用时钟判断过期。节点时钟偏移不只是提前/推迟接管：快时钟的竞争者会
  接管仍在运行的持有者的锁，**同时进入临界区**。请强制 NTP 同步或留出足够余量。
- `gorm` 的重复键识别只覆盖 MySQL（1062）与 PostgreSQL（23505）。
- Consul 在持有者死亡后先等 session TTL 过期，再加默认 lock-delay 才释放 key，实际故障转移
  时间大于 `LockTimeout`。
- `noop` 完全不提供排他性，只用于接线测试与本地运行；它仍遵守 `ErrLockNotHeld` 契约，并可
  用 `Lose()` 模拟丢锁。
- `gorm`/`mongo` 的 `Unlock` 会顺带删除仍带本令牌的过期行/文档，但持有者崩溃且不再调用
  `Unlock` 时不会自动清理；长期运行需要定期删除 `expired_at` 已过期的记录。
- MongoDB 每次 `Lock` 通过 `listIndexes` 校验当前集合。新文档以锁名作为 `_id`，并按
  `name` 匹配已有记录，旧 ObjectID 文档可以原地续期和接管。重建集合前须先停止持锁业务，
  重建后调用 `Init` 恢复索引。
