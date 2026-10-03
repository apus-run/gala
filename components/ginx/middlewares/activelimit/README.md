# Active Limit

| 包 | 作用域 | 限制方式 |
| --- | --- | --- |
| `locallimit` | 当前进程 | 活跃请求计数 |
| `ratelimit` | 当前进程、每个键 | 令牌桶请求速率 |
| `redislimit` | 多实例共享键 | 过期请求租约 |

## 本地速率限制

`ratelimit.NewRateLimit(window, requests).Build()` 使用令牌桶，补充速率为 `requests / window`，突发容量为 `requests`。参数必须为正，否则 Build 会 panic。配置 setter 在第一次 Build 前调用；创建新配置时使用新的实例。

同一实例生成的处理器共享缓存和新键初始化锁，确保并发首次访问同一个键只创建一个桶。生成的处理器固定配置和响应头，不随后续 setter 调用改变。缓存淘汰或 TTL 到期后会重新创建桶；该限制为本地限制，不提供跨副本的全局额度。缓存实现必须支持并发访问。

## Redis 并发限制

```go
redisClient := redis.NewClient(&redis.Options{
    Addr:                  "127.0.0.1:6379",
    ContextTimeoutEnabled: true,
    DialTimeout:           time.Second,
    ReadTimeout:           time.Second,
    WriteTimeout:          time.Second,
})
limiter := redislimit.NewRedisActiveLimit(redisClient, 100, "payments-active").
    SetLeaseTTL(30 * time.Second).
    SetOperationTimeout(time.Second)
router.Use(limiter.Build())
```

`redisClient` 需要提供 `Eval`，标准 go-redis Client、ClusterClient 和 UniversalClient 均可使用。Redis 账号需要执行 EVAL 以及脚本内 TIME、ZREMRANGEBYSCORE、ZSCORE、ZCARD、ZADD、ZREVRANGE、PEXPIREAT、ZREM 的权限。

每次请求使用独立 UUID，通过 Lua 原子清理过期租约、判断容量并申请名额。释放只删除该请求的标识，重复释放或旧请求迟到不会减少其它请求的名额。申请重试使用同一标识，避免响应丢失造成重复计数。Redis 服务端时间用于租约过期，整个键的 TTL 保持到最晚到期的租约。

默认租约为 **30 秒**，同时给下游请求的 `Request.Context()` 设置执行 deadline。业务处理器及其 HTTP、数据库、RPC 调用必须遵守该 context；Go 无法强制停止忽略取消的处理器。若处理器超过租约仍继续执行，过期回收后实际并发可能超出上限。长请求应配置足够的租约和明确的执行超时，流式请求需评估是否适合此机制。

默认每次 Redis 操作设置 **1 秒**的 context deadline。传入 go-redis 客户端需开启 `ContextTimeoutEnabled` 并配置有界的连接及读写超时；该开关默认关闭，仅传入 deadline 不会约束其网络读写。自定义客户端同样必须遵守 context。申请异常时返回 500，并尝试释放可能已成功创建的租约；容量不足时返回 429。释放采用独立的有界 context，不受请求取消影响；释放失败会记录日志，剩余租约在到期后回收。`SetLogFunc(nil)` 关闭日志。

配置 setter 在 Build 前调用，生成的处理器保存配置快照。`SetMaxActive` 可以并发修改容量；非正值拒绝新请求，已有请求继续完成。Redis 不可用时拒绝入场，不自动降级为无保护放行。

## 从旧计数器升级

旧版使用传入的 `key` 做 INCR/DECR；当前使用 **`key + ":leases"`** 存储有序集合。两套实现不会共享容量，**不要混合运行新旧版本并期待全局上限仍有效**。部署时应停止旧版本接收流量，等待在途请求结束，再统一启用新版本；迁移后旧计数键由应用按部署流程清理。

仅实现 INCR/DECR 的自定义客户端或测试替身需要改为支持 Eval。构造函数名称和参数顺序保持一致。

## 测试

```sh
go test -race ./middlewares/activelimit/...
```

Redis 测试使用 miniredis 执行 Lua，通过真实 go-redis 客户端验证申请、共享容量、取消释放、异常恢复、单独租约过期和重复操作，无需启动外部 Redis。
