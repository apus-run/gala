# Redis policy adapter and watcher

Gala 自行维护，源自 casbin/redis-adapter v3.6.0（`fb84906`）。使用 Casbin v3、go-redis v9；参考 Midway 4 的注入与失效通知职责划分，不兼容其 `policies` 字符串格式。

```go
import redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"

adapter, err := redisadapter.NewAdapter(&redisadapter.Config{
    Client: client, Key: "authz:tenant-a:rules",
})
if err != nil { return err }
```

## 配置与所有权

| 字段 | 说明 |
| --- | --- |
| Client | `redis.Cmdable`；非 nil 时忽略拨号配置；拒绝类型化 nil |
| Key | 默认 `casbin_rules`；不同模型/租户必须隔离 |
| Network / Address | 未注入 Client 时必填，例 `tcp` / `127.0.0.1:6379` |
| Username / Password / TLSConfig | 自行创建客户端时的认证与 TLS 参数 |

Adapter 只依赖 `redis.Cmdable` 提供的命令和脚本能力，不要求 Close、Subscribe、AddHook 或 Process。自行创建客户端时单独保存关闭函数，注入客户端不保存关闭能力。推荐注入已配置有限 DialTimeout/ReadTimeout/WriteTimeout 的客户端。Adapter.Close 仅关闭自行创建的连接；不会关闭注入客户端。关闭前停止所有操作。旧 NewAdapterWith… 构造保留兼容，新增代码使用 Config。

## 策略读写

- 默认 Redis List，每个元素是 `CasbinRule{PType,V0,…,V5}` JSON；保持既有 Go 格式。模型最多六个策略字段。内部空字段保留位置，尾部空字段沿用旧格式的填充语义。
- LoadPolicy 与过滤加载用一条 `LRANGE key 0 -1` 获取完整列表，不再先查询长度。空策略类型、`null`、错误字段类型等坏数据返回错误，不会进入 Casbin 触发 panic。
- 过滤读取在 Go 中解码 JSON 后按字段比较；过滤删除/更新在 Lua 中使用 cjson 解码后比较，保留空过滤值的通配语义。引号、反斜杠、`&`、HTML 字符和 Unicode 按原始值精确匹配。过滤写入先校验全部现存行，再修改，避免坏数据造成部分删除；Redis 权限或运行时失败仍不提供脚本回滚。
- 非空 SavePolicy 先完整序列化，在唯一临时 List 中构建快照，最后用 RENAME 发布。构建失败不影响正式键，读取只能看到旧或新完整快照。空策略使用单条 DEL，错误原样返回。
- 临时键使用相同 Redis hash tag，并保留随机后缀。普通键自动使用全名作为 tag；含花括号的键需有合法非空 tag（如 `authz:{tenant-a}:rules`），否则非空 SavePolicy 在写入前返回错误。临时命名空间 `{tag}:gala:authz:snapshot:*` 必须由适配器独占。
- 临时键构建时设置 5 分钟 TTL，发布前清除 TTL。失败时尽力清理；清理也被拒绝时可能遗留临时键。需要允许临时键及 EXISTS/RPUSH/PEXPIRE/PERSIST/RENAME/DEL 和脚本操作的 ACL。断连仍可能发生在提交之后；重试前按业务需要核实状态。
- SavePolicy 是覆盖操作。应用必须串行管理该策略域的写入，不能拿旧模型与其他写入并行覆盖。Adapter 不保留 Midway BaseAdapter 那样的可写本地 policies 缓存。
- 写方法不广播。使用 authz 时应用提交写入后调用 SyncPolicy；Authorizer 保持 AutoSave 关闭。
- 完整快照发布保证只覆盖 SavePolicy；多个增删调用和既有批量/过滤修改接口不自动提供失败回滚。多条规则必须同时完整可见时，在应用串行写入边界内构造完整模型并用 SavePolicy 发布。
- 支持 Casbin 的增删改与过滤接口；已过滤 Adapter 不能直接交给 authz 构造。不要在多个执行器之间共享有加载状态的 Adapter。

脚本与全量读取适合可控规模策略。大量规则需要评估内存和 Redis 阻塞时长，构建阶段峰值内存约为两份策略；本模块不提供分片加载或跨策略域事务。

## Watcher

Watcher 接收独立的最小接口，不要求完整 `UniversalClient`：

```go
type WatcherClient interface {
    Publish(context.Context, string, any) *redis.IntCmd
    Subscribe(context.Context, ...string) *redis.PubSub
}
```

`*redis.Client` 和 `*redis.ClusterClient` 均满足该接口。`redis.Cmdable` 包含 Publish，但不包含 Subscribe，因此不能单独作为 Watcher 的客户端契约。

```go
watcher, err := redisadapter.NewWatcher(ctx, client, "authz:tenant-a:changed")
if err != nil { return err }
defer watcher.Close()
// NewWatcher 返回前已收到 Redis 订阅确认。
// 将 watcher 注入 authz.WithWatcher，并配置 WithWatcherErrorHandler。
```

频道显式必填，与策略 key/model/租户域保持对应。go-redis 使用独立订阅连接，可复用应用客户端。启动 Context 只控制建立订阅；生命周期通过 Close 管理。Update 使用 5 秒发布 Context，只传递固定失效文本，不传策略或处理增量消息。

SetUpdateCallback 和消息读取受锁保护；回调在锁外执行。Close 幂等、等待当前回调结束、仅关闭订阅；不会关闭共享客户端。回调不能同步调用自己的 Close。应用应先停止 Update 再关闭 Watcher。

Pub/Sub 至多一次交付，重连或回调安装前可能丢失消息；发布成功不代表对端加载成功。必须配合 authz 的注册后补载、应用管理的 RunPolicyReload；有撤权期限时启用 WithMaxPolicyAge。Watcher 只改善延迟，不承担最终收敛保证。

## 验证

```sh
go test -race -count=1 ./...
```

使用 miniredis 覆盖临时构建每个写阶段故障、RENAME 失败、整份替换并发读取、清空错误、完整范围读取、注入资源所有权、订阅就绪、取消启动及 Close 等待回调。Docker 下已补充 Redis 7.4.11 单机和三主节点 Cluster 的快照、ACL 失败、订阅重连及过期恢复测试，结果为 PASS；详见 [Docker 验证记录](../../integration/README.md)。主从切换、长时间网络分区和大策略性能仍未覆盖。

详见 [PRD 与消融实验](../../prd-authz-upgrade.md)。
