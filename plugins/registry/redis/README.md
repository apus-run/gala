# registry/redis

在 Redis 上实现 `registry.Registry` 与 `registry.Discovery`：实例带按成员生效的租约，崩溃实例最迟一个租约后自动消失；发现侧用轮询 diff 替代 watch。

## 定位

- 依赖 gala 的 `components/rdb`：`NewRegistry` / `NewDiscovery` 接受 `rdb.Cmdable`——传 rdb Provider 的 `DB(ctx)` 结果，或任何 go-redis 客户端；其生命周期仍归调用方，两个角色独立 `Close`。
- 与 etcd / memory 后端实现同一契约，通过 `registrytest` 共享契约套件。
- 固定前缀 `gala:services:`（不可配置，与 etcd 后端的固定前缀一致）。

## 用法

```go
p, err := rdb.Connect(ctx, &rdb.Options{Addr: "127.0.0.1:6379"})
defer p.Close()

reg, err := redis.NewRegistry(p.DB(ctx)) // 或 WithLeaseTTL(15 * time.Second)
ins := &registry.ServiceInstance{
    Name:      "edu.course.student-api",
    Version:   "v1",
    Endpoints: []string{"grpc://10.0.0.1:9000"},
}
_ = reg.Register(ctx, ins) // ID 缺失时自动生成并写回
defer reg.Close()          // 下线所有仍在保活的实例，幂等

dis, err := redis.NewDiscovery(p.DB(ctx)) // 或 WithPollInterval(time.Second)
peers, _ := dis.GetService(ctx, "edu.course.student-api")
events, _ := dis.Subscribe(ctx, "edu.course.student-api") // 先重放快照，再推增量
```

## 存储契约

每个服务两个 key（固定段 `z:`/`h:` 区分类型，任何服务名——即使含 `:`——都不会让两种类型撞到同一个 key 上）：

| key | 结构 | 内容 |
|---|---|---|
| `gala:services:{<escaped-name>}:z` | zset | member = 实例 ID，score = 租约到期的 Unix 毫秒 |
| `gala:services:{<escaped-name>}:h` | hash | field = 实例 ID，value = 实例 JSON |

- `Register`（及心跳）原子地：清理过期成员 → 写入实例 → 续两个 key 自身的 TTL（以 Redis 服务端时间计算，到最后一个实例的租约截止时自动删除）。
- 一个心跳 goroutine 每 `LeaseTTL/3` 为所有已注册实例续约，用自己的 context，不随任何 Register 请求结束。
- `GetService` / 轮询惰性清理过期成员后读取存活实例。

对照 hertz-contrib/registry 的 redis 实现（hash + key 级 `EXPIRE`）：那种形态下实例崩溃后，只要兄弟实例还在续 key，死实例字段就永远留在 hash 里；本实现的租约按 member 生效，崩溃实例最迟一个 `LeaseTTL` 后不可见。

## 发现侧语义

Redis 没有 watch，`Subscribe` 用轮询 diff：订阅瞬间同步重放快照（缓冲区按快照大小加余量分配），此后每 `PollInterval`（默认 500ms）重读一次、按 ID diff 出 Add/Delete。轮询错误等下一 tick 重试——Redis 故障只延迟事件不终止订阅；只有 ctx 取消、`Close` 或缓冲区满（慢消费者）才关闭频道，与契约一致。

## 注意事项

- **租约时长**：默认 30s（`WithLeaseTTL` 可调，最小 300ms）。"崩溃到不可见"窗口为一个租约；心跳间隔为其三分之一。
- **短于租约的 Redis 故障**由心跳重试扛过；更长的故障让实例过期——实例确实不该再被发现。`Register` 的 Redis 写失败时会返回错误，已有注册继续按原内容续租。
- **score 用各应用主机的本地时钟**，跨机时钟偏差会等比放大/缩小租约窗口（与 etcd session 性质相同）。
- **实例 JSON 用 encoding/json/v2 编码**；混入键空间的外部成员会被读取路径跳过而不是报错。
