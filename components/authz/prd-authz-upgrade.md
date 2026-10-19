# PRD：authz 升级与策略一致性

- 更新日期：2026-10-19。
- 状态：需求与实现契约已对齐，已有实现及验收证据见第 9 节。
- 范围：`components/authz`、本地 GORM/Redis 适配器、Gin Casbin 归属中间件。
- 本文替代原 HTML 设计说明，保留原有行为和验收边界；不新增实现范围。

## 1. 背景与问题

应用实例使用内存策略鉴权。策略写入存储后，需要更新本实例和其他实例；通知可能丢失、订阅可能断开、策略读取可能失败。如果只等待下一次策略变更，旧授权可能无限期有效。

同时，资源“属于当前用户”与“用户获准执行动作”是两个独立条件。仅凭归属判断放行，会绕过策略授权。整份策略替换或过滤撤权若只完成一部分，也可能导致实际授权结果偏离管理操作。

本次升级面向接入授权组件的应用开发者、策略管理服务维护者及测试人员，提供完整快照发布、可恢复的多实例同步、明确的策略过期边界，以及资源归属授权。

## 2. 目标与成功标准

1. 策略写入提交后，本实例能够重载，并独立向其他实例发布失效提示。
2. 通知丢失或一次读取失败后，无需新的策略变更，也能由周期重载恢复。
3. 启用最大策略年龄后，年龄达到阈值的策略不能继续授予新请求权限。
4. 整份策略发布失败时保留旧的完整快照，避免残缺的 allow/deny 组合造成误放行。
5. `own` 必须同时满足策略授权和归属判断；所有组合满足明确的真值表。
6. 注入资源由应用管理，组件只关闭自己创建的资源。
7. 核心机制具有可复现的消融、回归或集成测试；测试结果与声明的验证范围一致。

不承诺故障情况下的固定收敛时限。启用最大策略年龄提供的是过期拒绝边界，不是集群强一致性。

## 3. 用户故事与验收标准

以下清单定义验收要求；实际执行结果及未覆盖范围单独记录在第 9、10 节。

### US-001:接入统一授权执行器

作为应用开发者，我希望通过模型和存储适配器构造授权器，使业务请求只读取已加载的内存策略。

- [ ] `New`、`NewAuthz`、`NewRedis` 返回实例前完成初次加载。
- [ ] 已过滤的 Adapter 被拒绝；支持过滤但当前未过滤的 Adapter 可以使用。
- [ ] `Enforce` 不访问 Adapter，不等待 Watcher。
- [ ] AutoSave 关闭，应用负责策略写入、事务、管理权限和审计。

### US-002:同步已提交的策略变更

作为策略管理服务维护者，我希望本地读取故障不阻止健康实例收到策略失效提示。

- [ ] `SyncPolicy` 独立尝试本地重载和通知，通知在重载锁外执行。
- [ ] 两侧失败原因通过 `errors.Join` 保留，能够用 `errors.Is/As` 检查。
- [ ] 无 Watcher 时仅执行重载；同步回声 Watcher 不造成自锁。
- [ ] Watcher 订阅就绪后，构造顺序为初次加载、注册回调、补载。
- [ ] 注册或补载失败时返回阶段错误，实例不交出。

### US-003:恢复漏通知并限制旧授权寿命

作为应用运行维护者，我希望周期任务修复漏通知，并在持续读取失败时拒绝过期策略。

- [ ] 应用可启动、取消并等待 `RunPolicyReload`；组件不自动启动该任务。
- [ ] 周期读取失败会被报告，后续 tick 继续尝试，且不广播。
- [ ] 启用阈值 T 时，策略年龄 `>= T` 后授权及角色查询均返回 `ErrPolicyStale`。
- [ ] 失败加载不更新时间，成功加载从其开始时刻计算年龄。
- [ ] 运行中的任务退出会被监督；退出时排空请求、写入和同步任务后再关闭资源。

### US-004:安全发布和维护策略

作为策略管理服务维护者，我希望保存失败或过滤值含特殊字符时，存储状态仍与操作结果一致。

- [ ] Redis 非空整份保存先构建同槽临时 List，再通过 RENAME 发布；空策略使用单条 DEL。
- [ ] GORM 整份保存的 DELETE 与分批 INSERT 位于同一事务，插入失败回滚旧策略。
- [ ] 过滤读取、删除和更新按解码后的字段比较，引号、反斜杠、`&`、HTML 字符和 Unicode 均按字面值匹配。
- [ ] 过滤写入在修改前校验全部现存行，坏数据不能造成部分删除。
- [ ] null、空策略类型或错误字段类型返回错误，不触发重载 panic。

### US-005:按资源归属授权

作为 API 开发者，我希望区分任意资源访问与自有资源访问，并保持认证与授权职责清晰。

- [ ] `any` 仅检查 `action:any`，不调用 `IsOwn`。
- [ ] `own` 同时要求 `action:own` 和 `IsOwn`，策略拒绝时不调用 `IsOwn`。
- [ ] `own-or-any` 先检查 any，允许即短路，否则执行 own 两步。
- [ ] 8 种布尔输入 × 3 种归属模式的结果与 `IsOwn` 调用次数符合契约。
- [ ] 非空要求中的无主体、拒绝及内部错误分别走现有 401、403、500 处理。

### US-006:使用清晰的依赖与资源边界

作为组件维护者，我希望执行器、存储和 HTTP 适配保持高内聚、低耦合。

- [ ] Redis Adapter 与 `NewRedis` 仅要求 `redis.Cmdable`。
- [ ] Watcher 仅要求 `Publish` 和 `Subscribe`，不要求完整客户端或 Close 能力。
- [ ] 注入客户端不会被 Adapter 或 Watcher 关闭。
- [ ] Adapter 自建客户端有显式关闭入口；Watcher.Close 幂等并等待订阅循环退出。
- [ ] `NewAuthz` 使用本地 GORM 模块，Gin 中间件不导入 authz。

### US-007:端到端验证策略变更与恢复

作为测试人员，我希望使用隔离的真实服务验证应用提交策略到对端授权结果的完整后端流程。

依赖 US-001 至 US-006；HTTP 归属组合另由中间件测试验证，不将其描述为同一条 Docker 端到端链路。

- [ ] 自动化流程覆盖存储提交、`SyncPolicy`、Watcher、对端重载和 `Enforce` 结果变化。
- [ ] 覆盖漏通知后周期恢复，以及至少一个写入或读取故障路径。
- [ ] 在 Redis 单机、三主节点 Cluster、MySQL 和 PostgreSQL 上验证对应契约。
- [ ] 测试使用独立数据并清理本次容器、网络及匿名卷。
- [ ] 仓库提供可执行的复现入口及运行日志，不以单元测试替代真实服务验证。

## 4. 功能需求

### 4.1 构造、鉴权与策略重载

- FR-1:系统必须在初次加载成功后才交出 Authorizer。
- FR-2:系统必须拒绝 `IsFiltered()` 已为 true 的 Adapter。
- FR-3:系统必须让 `Enforce` 只读取内存策略。
- FR-4:系统必须通过 `Authorizer.mu` 串行化整次读取和模型应用。
- FR-5:系统必须在加载失败时保留上一份成功策略。
- FR-6:系统必须保持 AutoSave 关闭。
- FR-7:系统必须保持既有 `New`、`NewAuthz`、`Enforce`、`LoadPolicy`、`GetRolesForUser` 签名兼容。

### 4.2 通知、恢复与新鲜度

- FR-8:系统必须直接注册 Watcher 回调，不通过 Casbin `SetWatcher` 挂载。
- FR-9:系统必须在回调注册后补载一次策略。
- FR-10:系统必须要求 Watcher 与错误处理函数成对配置。
- FR-11:系统必须拒绝类型化 nil Watcher。
- FR-12:系统必须让回调忽略消息内容，仅触发完整重载。
- FR-13:系统必须在本地重载失败时仍尝试通知。
- FR-14:系统必须在重载锁外调用 Watcher.Update。
- FR-15:系统必须聚合保留本地加载和通知的失败原因。
- FR-16:系统必须提供由应用管理生命周期的周期重载入口。
- FR-17:系统必须在周期任务取消时返回 `ctx.Err()`。
- FR-18:系统必须在策略年龄达到配置阈值时拒绝授权及角色查询。
- FR-19:系统必须只在成功加载后更新年龄基准。
- FR-20:系统必须以该次成功加载的开始时刻作为年龄基准。

### 4.3 存储与资源

- FR-21:系统必须以单次 `LRANGE key 0 -1` 读取 Redis 策略列表。
- FR-22:系统必须在 Redis 临时快照完整后才替换正式键。
- FR-23:系统必须返回清空策略时的存储错误。
- FR-24:系统必须在同一 GORM 事务内完成整份策略删除和插入。
- FR-25:系统必须按解码后的字段值执行策略过滤。
- FR-26:系统必须将坏策略报告为错误，避免传入 Casbin 导致 panic。
- FR-27:系统必须保留调用方对注入客户端、数据库及 Watcher 的所有权。
- FR-28:系统必须等待 Redis 订阅确认后才从 `NewWatcher` 返回。

### 4.4 Gin 归属授权

- FR-29:系统必须要求 own 模式先通过策略授权。
- FR-30:系统必须在 any 模式下禁止配置 `IsOwn`。
- FR-31:系统必须在 own 与 own-or-any 模式下要求配置 `IsOwn`。
- FR-32:系统必须在 any 已允许时跳过 own 检查。
- FR-33:系统必须克隆传入的 Access 列表，隔离后续切片修改。
- FR-34:系统必须支持按顺序短路的 MatchAllRule 和 AtLeastOneRule。
- FR-35:系统必须对空 Access 列表放行且不查询主体。
- FR-36:系统必须在构造 Handler 时拒绝空资源、空动作、含冒号动作及非法 Possession。

## 5. 接口与行为契约

### 5.1 接口

```go
// authz
WithWatcher(persist.Watcher)
WithWatcherErrorHandler(func(error))
WithMaxPolicyAge(time.Duration)
(*Authorizer).SyncPolicy() error
(*Authorizer).RunPolicyReload(context.Context, time.Duration, func(error)) error
ErrPolicyStale

// adapter/redis
NewWatcher(context.Context, WatcherClient, string) (*Watcher, error)
(*Watcher).SetUpdateCallback(func(string)) error
(*Watcher).Update() error
(*Watcher).Close()
(*Adapter).Close() error

type WatcherClient interface {
    Publish(context.Context, string, any) *redis.IntCmd
    Subscribe(context.Context, ...string) *redis.PubSub
}
```

`WithMaxPolicyAge(0)` 禁用过期，负值非法。为兼容现有应用，默认仍为 0；此时不具备撤权时间界限。配置 Watcher 时，应用须保证其在 New 前订阅就绪。一个 Watcher 只绑定一个 Authorizer，应用不能再次覆盖其回调。

### 5.2 运行顺序

```text
构造：订阅就绪 → 初次加载 → 注册回调 → 补载 → 返回实例
变更：应用提交存储 → SyncPolicy → 本地重载 → 锁外通知 → 聚合错误
接收：Watcher → LoadPolicy → 失败交给 onError，不广播
恢复：应用 RunPolicyReload → 立即/周期 LoadPolicy，不广播
鉴权：检查策略年龄 → 内存 Enforce → 允许、拒绝或错误
```

`SyncPolicy` 返回成功只表示本地加载与发布没有报错，不代表所有对端已经加载。无 Watcher 时仅重载。组件不参与应用写事务，不内置策略管理、审计或建表流程。

### 5.3 周期与过期边界

RunPolicyReload 先创建 ticker，再立即加载；每次加载结束后等待 ticker，而不是再等一个完整间隔。

设 ticker 间隔为 I、单次读取及应用耗时为 L。在忽略锁竞争、错误处理和调度延迟且 L 稳定的条件下：

- L < I：两次加载起点大约相隔 I。
- L >= I：加载结束后可能立即取得已到期的 tick，起点间隔接近 L。
- I+L 不是稳定周期。只有等待下一次加载不超过 I、该次加载不超过 L 时，它才可作为恢复耗时的估计上界。

启用 T 后，过期条件为 `time.Since(最近一次成功加载的开始时刻) >= T`。相等时即拒绝；一次成功加载本身耗时已不小于 T，且没有更新的成功加载时，下一次鉴权也会拒绝。

RunPolicyReload 在启用 T 时只检查 I < T，不检查或限制单次加载耗时。取消不能中断已经进入的 `persist.Adapter.LoadPolicy`；应用须配置有限存储超时，并为读取、模型应用和调度留余量。

年龄控制依赖权威存储，不能识别“读取成功但读到滞后副本”。过期不会中止已进入业务 Handler 的操作；严格写入授权仍须由业务事务约束。

### 5.4 失败语义

| 失败 | 可观察行为 |
| --- | --- |
| 回调注册或补载失败 | New 返回 nil 和阶段错误；补载错误包含 `authz: watcher catch-up`；应用仍须关闭 Watcher |
| 加载失败 | 保留旧策略，不更新时间；到期后拒绝 |
| 本地加载和通知都失败 | `errors.Join` 保留两侧原因 |
| 回调加载失败 | 在重载锁外调用 onError；恢复交给独立周期任务 |
| 周期任务取消 | 返回 `ctx.Err()`；等待已进入的加载结束 |
| 策略年龄 >= T | Enforce 返回 `false, ErrPolicyStale`，角色查询返回 `nil, ErrPolicyStale` |
| Watcher 关闭 | 幂等，关闭自身订阅并等待订阅循环退出，不关闭注入客户端 |

错误处理函数须线程安全、及时返回，不同步重入加载、通知或关闭。Watcher 回调不能调用自身 Close。应用须监督周期任务退出，在开始接收流量前建立受管理任务。

### 5.5 归属契约

| 模式 | 允许条件 | IsOwn |
| --- | --- | --- |
| PossessionAny | `Enforce(subject, resource, action+":any")` | 必须 nil，不调用 |
| PossessionOwn | own 策略允许且归属成立 | 必填，仅策略允许后调用 |
| PossessionOwnOrAny | any 允许，或 own 策略允许且归属成立 | 必填；any 允许即短路 |

策略动作存 `read:any`、`read:own` 等，不存 `own|any`。create/read/update/delete 提供常量，动作仍可为无冒号的自定义字符串。资源可以含冒号。

多个 Access 默认全部满足，可选择 AtLeastOneRule；按顺序在决定结果或遇到错误时短路。空列表跳过身份查询，与既有 RequiresPermissions 一致。沿用 Option 校验，PermissionParser 不参与 Access 求值。

主体为空走 401，拒绝走 403，Enforce/IsOwn 错误走现有 500。ErrPolicyStale 默认同样走 500，应用可映射为 503。

IsOwn 必须使用同一认证主体、租户及后续 Handler 操作的资源，不能信任请求自报 owner。可变归属的写操作须在事务或 WHERE owner_id 条件中再次约束，布尔归属结果不能消除 TOCTOU。认证留在前置 authn 中间件，主体由 SetLookup 提供。

## 6. 技术约束与取舍

### 6.1 职责与依赖

- Authorizer 持有执行器，负责初次加载、鉴权、串行重载、回调注册和通知。
- Gin 中间件把请求及归属规则转换为授权条件，不导入 authz。
- 应用负责策略写入、提交后调用 SyncPolicy、主体/归属提取、任务监管和资源关闭。
- NewAuthz 使用本地 `github.com/apus-run/gala/components/authz/adapter/gorm`；来源为上游 v3.41.0，已修复整份保存事务边界和空策略类型校验。
- NewRedis 保留便利入口，推荐显式 Adapter 注入。当前 authz 模块仍含 GORM/Redis 存储依赖，不宣称依赖图完全存储无关。
- 使用 Casbin v3.10.0、go-redis v9.23.0；上游来源及许可证保留在适配器模块。

### 6.2 Redis 策略格式与发布

默认键 `casbin_rules`，类型为 List，每个元素是 `PType,V0…V5` 的 JSON 对象，延续现有 Go 格式，不兼容 Midway 的 `policies` 单键 JSON。自定义 key 通过 Config 注入。内部空字段保留位置，尾部空字段沿用填充语义。

非空 SavePolicy 完整序列化后，在唯一的临时 List 中构建快照，最后通过 RENAME 发布；空策略使用单条 DEL。构建失败不改正式键，断连仍可能丢失成功响应，不能将“返回错误”理解为“没有提交”。

临时键为 `{hash-tag}:gala:authz:snapshot:随机值`，与正式键同槽。普通键使用全名作 tag；已有非空 hash tag 则复用。含空或不完整花括号的 key 不能执行非空 SavePolicy，可使用 `authz:{tenant-a}:rules` 等合法 key。临时命名空间由适配器独占。

临时键构建时设置 5 分钟 TTL，发布前 PERSIST；正式键不继承临时 TTL。错误时尽力清理，清理同时被拒绝时可能残留临时键。ACL 需允许临时命名空间、脚本及 EXISTS/RPUSH/PEXPIRE/PERSIST/RENAME/DEL。峰值内存约为两份策略；脚本执行会阻塞 Redis，策略规模须可控。

Go 过滤读取和 Lua 过滤写入均解码 JSON 后比较字段。Filter 结构的空切片不约束该字段，非空切片精确匹配其中任一值；RemoveFilteredPolicy/UpdateFilteredPolicies 的空字符串过滤值仍为通配。过滤写入先校验全部现存行，再修改。

完整快照发布保证只覆盖 SavePolicy。多个增删调用及批量/过滤修改不自动组成可回滚事务；多条规则必须完整可见时，应用应串行构造完整模型并通过 SavePolicy 发布。禁止用旧内存整份覆盖其他并发写入。Adapter 写方法不隐式广播。

### 6.3 GORM 保存与建表

NewAuthz 不自动迁移，应用须提前建表。整份保存使用同一事务内的 DELETE 和分批 INSERT；插入失败或取消保存时回滚旧快照，后续保存可以恢复。

不用 MySQL 会隐式提交的 TRUNCATE，因此保存不重置 ID 序列。需继续维护自定义表名和跨批次保存。坏策略类型返回错误，不能导致 Preview 或加载 panic。

### 6.4 Watcher 与生命周期

Adapter.Config.Client、内部命令字段及 NewRedis 使用 `redis.Cmdable`。Watcher 使用只含 Publish/Subscribe 的 WatcherClient；Cmdable 含 Publish，但没有 Subscribe。

NewWatcher 等待订阅确认。启动 Context 只控制建立订阅，生命周期通过 Close 管理。Adapter 与 Watcher 可复用同一 go-redis 客户端，订阅使用独立连接。频道显式配置并对应同一 key/model/租户域；List 写入本身不会代替发布失效消息。

Watcher 只发布固定失效文本。Update 使用 5 秒发布 Context；客户端还须配置有限读写、拨号超时。Pub/Sub 为至多一次交付，重连可能丢消息，周期重载必须独立存在。

Authorizer 没有 Close。退出顺序：停止接收并排空请求与写入 → 取消并等待周期任务及 SyncPolicy → Watcher.Close 等待回调 → 关闭数据库/Redis 客户端。Adapter 自建客户端通过独立关闭函数释放，注入客户端不要求 Close 能力。

### 6.5 Midway 本地源码对照

对照 `/Users/a08/Downloads/midway-4-next/packages/casbin` 和 `packages/casbin-redis-adapter`，仅描述该本地版本。

| 参考 | 采纳内容 | Go 实现的取舍 |
| --- | --- | --- |
| casbin/src/enforcer.service.ts | 模型、Adapter、Watcher 注入 | 不代理整个 Enforcer；显式注册回调适配 Go WatcherEx |
| casbin/src/adapter.ts | 策略行与过滤职责 | 不缓存可写 policies 后用旧内存整份覆盖 |
| casbin-redis-adapter/src/adapter.ts | 应用注入客户端 | 保留 Go List 格式，不复制 DEL + SET |
| casbin-redis-adapter/src/watcher.ts | 独立失效频道 | 等待订阅确认，错误按 Go error 返回 |
| casbin/src/auth.guard.ts | resource/action/possession 和短路 | own 必须先有策略，不能只凭 isOwn 放行 |

## 7. 范围之外

- 不提供强一致授权、集群加载确认、跨库存储事务或全局线性一致性。
- 不增加策略管理服务、增量消息协议、分布式锁或进程 ID 自通知过滤协议。
- 不增加隐式周期任务、Authorizer.Close 或 WithModelText。
- 不内置 root、用户表、登录认证或特定租户模型；应用在模型中定义 g2、root matcher 等规则。
- 不改变 RoutePermission、RequiresPermissions、RequiresRoles 的既有路径；角色查询仍是直接角色。
- 不迁移为 Midway Redis 数据格式，不提供分片加载或跨策略域事务。

## 8. 应用接入参考

完整示例来自 [examples/supervision/supervision.go](examples/supervision/supervision.go)，以下代码与该文件一致。serve 必须在取消后停止接收并排空请求、策略写入与 SyncPolicy 后返回；client 由调用方在 Run 返回后关闭。

```go
// Package supervision demonstrates application-owned policy reload supervision.
package supervision

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/apus-run/gala/components/authz"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

// Run serves until cancellation or either task exits. serve must stop accepting
// work on cancellation and drain requests, policy writes and SyncPolicy calls
// before returning. The caller closes client only after Run returns.
// Run is an example for one policy namespace; use your own key and channel.
func Run(ctx context.Context, client interface {
	redis.Cmdable
	redisadapter.WatcherClient
}, modelPath string,
	serve func(context.Context, *authz.Authorizer) error,
) error {
	watcher, err := redisadapter.NewWatcher(ctx, client, "authz:tenant-a:changed")
	if err != nil {
		return err
	}
	defer watcher.Close()
	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{
		Client: client, Key: "authz:tenant-a:rules",
	})
	if err != nil {
		return err
	}
	az, err := authz.New(
		authz.WithModelFilePath(modelPath),
		authz.WithPolicyAdapter(adapter),
		authz.WithWatcher(watcher),
		authz.WithWatcherErrorHandler(func(err error) { log.Print(err) }),
		authz.WithMaxPolicyAge(30*time.Second),
	)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- az.RunPolicyReload(runCtx, 5*time.Second, func(err error) { log.Print(err) }) }()
	go func() { results <- serve(runCtx, az) }()

	first := <-results // Observe either task's exit while the application is running.
	cancel()           // Stop the sibling task and wait for it exactly once.
	second := <-results
	var failures []error
	for _, err := range []error{first, second} {
		if err != nil && !errors.Is(err, context.Canceled) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
```

归属中间件示例：

```go
handler, err := builder.RequiresAccess([]casbinmw.Access{{
    Resource: "documents", Action: casbinmw.ActionRead,
    Possession: casbinmw.PossessionOwnOrAny,
    IsOwn: func(c *gin.Context) (bool, error) {
        return repository.IsOwner(c.Request.Context(), trustedSubject(c), c.Param("id"))
    },
}})
```

该片段中的 repository/trustedSubject 由应用提供，策略值为 `read:any` 或 `read:own`。

## 9. 验收证据与成功度量

成功度量采用明确输入/输出与故障后的状态，不编造延迟、吞吐或覆盖率目标。验收以策略结果正确、故障不误放行、恢复不依赖新消息、资源所有权不越界为准。

### 9.1 消融与回归

消融 PASS 表示有/无机制产生预期差异，不表示删除机制仍然安全。回归和集成测试与消融实验分开解释。

| 机制或问题 | 验证结果 | 证据 |
| --- | --- | --- |
| 整次重载锁 | 去掉锁时 Adapter 读取可重叠，保留时串行；不声称已复现快照倒序覆盖 | [ablation_test.go](ablation_test.go)：TestAblationWholeLoadSerialization |
| 显式回调 | Casbin SetWatcher 对 WatcherEx 不装回调，Authorizer 显式注册后存在回调 | [ablation_test.go](ablation_test.go)：TestAblationWatcherExNeedsExplicitCallback |
| 启动补载 | 相同 Watcher 和撤权时机，只改变是否补载；另验证生产构造顺序 | [ablation_test.go](ablation_test.go)：TestAblationStartupCatchUp；[sync_test.go](sync_test.go)：TestWatcherCatchUpClosesStartupGap |
| 策略过期 | 相同初始策略及读取故障，仅改变 WithMaxPolicyAge；等待真实时间后调用 Enforce | [ablation_test.go](ablation_test.go)：TestAblationPolicyExpiry |
| 精确时间边界 | 虚拟时钟下 T−1ns 有效、T 时授权和角色查询拒绝；成功慢加载已过期，快速重载恢复 | [timing_contract_test.go](timing_contract_test.go)：TestPolicyExpiresAtExactMaximumAge、TestSuccessfulSlowLoadDoesNotRenewAgeFromCompletion |
| ticker 与构造失败 | 验证 L<I、L>I 的起点间隔；I>=T 在读取前拒绝；补载失败不交出实例 | [timing_contract_test.go](timing_contract_test.go)：TestReloadTickerUsesFixedTicksNotPostLoadDelay、TestReloadIntervalAtMaximumAgeRejectedBeforeLoading、TestWatcherCatchUpFailureDoesNotReturnAuthorizer |
| 本地故障与通知解耦 | 本地失败仍使健康 peer 撤权，保留加载及通知两种错误 | [sync_test.go](sync_test.go)：TestSyncPolicyNotifiesDespiteLocalFailureAndPreservesBothErrors |
| 锁外通知 | 同步回声可完成，不自锁 | [sync_test.go](sync_test.go)：TestSyncPolicyAllowsSynchronousEcho |
| 周期恢复 | 无新事件时，读取故障恢复后由下一 tick 撤权；取消后退出 | [sync_test.go](sync_test.go)：TestPeriodicReloadRecoversAfterErrorWithoutNewEvent |
| 完整 Redis 读取 | 在读取前将空 List 替换为三条策略，完整读到三条，不使用 LLEN | [adapter/redis/consistency_test.go](adapter/redis/consistency_test.go)：TestLoadUsesSingleFullRange |
| 整份发布及失败 | 并发读仅见完整快照；空保存故障返回错误；五个构建/发布失败阶段不改正式策略 | [adapter/redis/consistency_test.go](adapter/redis/consistency_test.go)：TestSavePolicyReadersSeeWholeSnapshots、TestSaveEmptyPolicyPropagatesFailure、TestSaveFailurePreservesCompleteSnapshot |
| 发布机制对照 | 测试现场重建 DEL 加循环 RPUSH，第二次 RPUSH 失败后仅剩 allow 并误放行；当前 SavePolicy 保留 allow/deny 两条并拒绝。没有执行旧版 SavePolicy 函数 | [redis_failure_test.go](redis_failure_test.go)：TestAblationRedisSnapshotPublication、TestFailedRedisSaveCannotRemoveDenyOnReload |
| 特殊字符过滤 | 过滤读、更新、撤权精确匹配，保留无关策略 | [adapter/redis/filter_regression_test.go](adapter/redis/filter_regression_test.go) |
| 坏策略回调 | 返回错误、保留旧授权结果，不刷新新鲜度，数据修复后可加载 | [redis_invalid_policy_test.go](redis_invalid_policy_test.go) |
| GORM 保存回滚 | 插入错误及取消不丢旧策略，重试成功；覆盖分批、自定义表名、清空及坏类型 | [adapter/gorm/save_regression_test.go](adapter/gorm/save_regression_test.go) |
| 组件协作 | miniredis、两个 Watcher、两个 Authorizer，提交撤权并同步到对端 | [redis_sync_test.go](redis_sync_test.go) |
| 归属组合 | 8×3 结果与 IsOwn 调用次数，401/403/500、短路及配置副本 | [access_test.go](../ginx/middlewares/casbin/access_test.go) |
| 任务监督 | 运行中观察退出，取消另一任务并只等待一次；覆盖服务错误和父 Context 取消 | [监督示例](examples/supervision/supervision.go)、[生命周期测试](examples/supervision/supervision_test.go) |

旧 A4 声称第一次仍持 RLock 时，第二次加载已完成模型应用；该时序不成立，证明已撤回。现有锁消融只证明整次读取串行化，不将其扩写为未观察到的快照倒序结果。

### 9.2 复现命令

从仓库根目录逐条执行；使用独立子 shell，避免前一条 cd 改变后一条路径。

```sh
(cd components/authz && go test -race -count=1 ./...)
(cd components/authz/adapter/redis && go test -race -count=1 ./...)
(cd components/authz/adapter/gorm && go test -race -count=1 -run 'Test(SavePolicyFailureRollsBackDeletion|SavePolicyCustomTableAcrossBatches|LoadPolicyRejectsEmptyPolicyType|NilField)$' .)
(cd components/ginx && go test -race -count=1 ./middlewares/casbin)
python3 components/authz/integration/run.py
```

### 9.3 Docker 集成结果

2026-10-19 已验证：authz（含监督示例）、Redis 的 race 测试及本地 GORM 回归测试通过；此前 Gin race 测试通过。本轮适配器 go vet 通过。验证环境为 Go 1.27.0、Casbin v3.10.0、go-redis v9.23.0，另含 SQLite 和 miniredis 回归。

Docker 使用 Redis 7.4.11、MySQL 8.4.11、PostgreSQL 16.15。最终 Docker race 测试 PASS，Go 测试进程耗时 3.620s，不含容器启动和清理；该记录不作为性能指标。

- [复现说明与镜像摘要](integration/README.md)
- [Docker 运行入口](integration/run.py)
- [完整运行日志](integration/results-2026-10-19.txt)
- [集成测试源码](docker_integration_test.go)

| 真实服务场景 | 验证内容与证据 |
| --- | --- |
| Redis 单机及三主节点 Cluster | TestDockerRedisSnapshotsAndWatchers：并发完整快照、两实例撤权、漏通知后周期修复及注入客户端所有权 |
| 三个主节点槽位范围 | TestDockerClusterPublishesInEveryMasterSlotRange：分别发布、加载和清空，同槽暂存及 RENAME 成功 |
| ACL 拒绝 | TestDockerRedisACLFailuresPreserveDeny：禁用 RPUSH/PEXPIRE/PERSIST/RENAME/DEL 后 allow/deny 仍完整，重载仍拒绝 |
| 订阅断开重连 | TestDockerRedisWatcherReconnect：CLIENT KILL 后自动重连，撤权通知恢复 |
| 读取故障 | TestDockerRedisReadFailureExpiresAndRecovers：禁用 LRANGE 后过期拒绝，权限恢复后重载成功 |
| 特殊字符及坏数据 | TestDockerRedisLiteralFilteringAndMalformedPolicy：单机与 Cluster 精确过滤、更新、撤权，坏数据加载失败保留旧快照 |
| MySQL/PostgreSQL | TestDockerGORMCommittedPolicyAndTimeout：不自动建表、显式迁移、事务可见性、角色、跨实例同步、表锁超时与恢复；CHECK 约束失败及取消保存保留旧快照 |

运行脚本创建独立 Compose 项目和随机回环端口，最后清理本次容器、网络与匿名卷。测试中三个 Redis 主节点运行于同一容器，无副本。不能将该环境描述为已验证主从故障转移。

## 10. 未决事项与验证限制

当前行为契约没有待确认项。以下内容未被现有验证覆盖，不能据此宣称已经满足：

- Redis 主从故障转移、长时间网络分区、大策略容量及性能压测。
- SQL Server 和迁入 GORM 适配器的完整上游测试集。
- 应用自身业务事务中的严格写权限、可变资源归属及滞后副本控制。
- CI 是否已经接入这些命令；现有记录是本地与 Docker 执行结果。

## 11. 代码索引与修订记录

| 路径 | 职责 |
| --- | --- |
| [authz.go](authz.go)、[options.go](options.go)、[roles.go](roles.go) | 构造、串行重载、注册补载、新鲜度和角色查询 |
| [sync.go](sync.go) | 独立加载/通知、周期重载、ErrPolicyStale |
| [GORM Adapter](adapter/gorm/adapter.go) | 本地适配器、事务保存和格式校验 |
| [Redis Adapter](adapter/redis/adapter.go) | List 快照、字段过滤和客户端所有权 |
| [Redis Watcher](adapter/redis/watcher.go) | 订阅就绪、发布及订阅生命周期 |
| [Gin Access](../ginx/middlewares/casbin/access.go) | 归属组合和构造校验 |
| [监督示例](examples/supervision/supervision.go) | 应用管理任务退出及资源关闭 |

2026-10-19：对照 Midway 确定职责和归属语义；完成多实例同步、恢复与过期边界；修复 Redis 发布及过滤、GORM 保存事务和坏数据校验；保留本地适配器与最小客户端接口；撤回旧 A4 证明；补齐独立消融、时序、监督示例和 Docker 证据。本文将已有 HTML 说明重组为 PRD，引用与实现契约同步保留。
