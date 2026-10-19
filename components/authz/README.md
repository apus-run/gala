# authz

基于 Casbin 的授权组件，支持模型文件、文件或数据库策略存储、角色查询和串行重载。HTTP 路由、权限列表及角色校验请使用 [Gin Casbin 中间件](../ginx/middlewares/casbin/README.md)。

## 安装

要求 **Go 1.27 或更高版本**，使用 **Casbin v3**。

```sh
go get github.com/apus-run/gala/components/authz
```

以下接口与示例对应当前仓库源码。使用尚未发布的变更时，在应用的 `go.mod` 中通过 `replace` 指向本地 `components/authz` 模块。

## 接口签名

```go
func New(opts ...Option) (*Authorizer, error)
func NewAuthz(db *gorm.DB, opts ...Option) (*Authorizer, error)
func NewRedis(client redis.Cmdable, opts ...Option) (*Authorizer, error)

func (a *Authorizer) Authorize(sub, obj, act string) (bool, error)
func (a *Authorizer) Enforce(rvals ...any) (bool, error)
func (a *Authorizer) GetRolesForUser(name string, domain ...string) ([]string, error)

func (a *Authorizer) LoadPolicy() error
func (a *Authorizer) SyncPolicy() error
func (a *Authorizer) RunPolicyReload(ctx context.Context, interval time.Duration, onError func(error)) error
```

| 构造入口 | 用途 |
| --- | --- |
| `New(opts...)` | 使用配置中的 PolicyAdapter；未设置时从 `./policy.csv` 加载 |
| `NewAuthz(db, opts...)` | 从传入的非 nil GORM 数据库创建本地 `adapter/gorm`，覆盖配置中的 PolicyAdapter |
| `NewRedis(client, opts...)` | 从传入的非 nil go-redis 客户端创建本地 `adapter/redis`，默认键 `casbin_rules`，不关闭客户端 |

三个入口都在返回实例前完成初始策略加载；配置 Watcher 后还会在回调注册完成后补一次加载。New 不接受 IsFiltered() 已为 true 的 Adapter，因为 Casbin 会跳过它的初始加载；支持过滤但当前未过滤的 Adapter 可以正常使用。NewAuthz 不自动迁移，应用必须提前创建策略表。构造可能返回模型文件、策略加载或数据库初始化错误，应先检查错误再使用实例。错误包含阶段信息，原始原因通过 `%w` 保留，可用 errors.Is / errors.As 检查。

## Option

| 选项 | 说明 | 默认值 |
| --- | --- | --- |
| `WithModelFilePath(string)` | 设置模型文件路径，不能显式设置为空 | `./model.conf` |
| `WithPolicyAdapter(persist.Adapter)` | 设置 New 使用的策略 Adapter，不能显式设置为 nil | `./policy.csv` 的文件 Adapter |
| `WithPolicyLoadTimeout(time.Duration)` | 限制 NewAuthz 的每次 GORM 策略读取耗时，必须大于零 | 5 秒 |
| `WithWatcher(persist.Watcher)` | 注入已订阅就绪、当前实例独占的 Watcher | 无 |
| `WithWatcherErrorHandler(func(error))` | 回调重载失败处理，与 Watcher 成对配置 | 无 |
| `WithMaxPolicyAge(time.Duration)` | 成功重载的最大年龄，超时拒绝授权与角色查询 | 0，禁用 |

选项按传入顺序应用，后面的同类设置覆盖前面的设置。NewOptions 从独立默认值开始组装；Validate 只检查最终状态，不补默认值或创建资源。组件不启动后台任务。

```go
az, err := authz.New(
    authz.WithModelFilePath("./model.conf"),
    authz.WithPolicyAdapter(fileadapter.NewAdapter("./policy.csv")),
)
```

文件路径相对于程序工作目录。组件不会自动创建模型文件或策略文件。NewAuthz 使用数据库 Adapter 覆盖选项中的 Adapter，但其它选项仍应满足 Validate 的约定。

## 模型与策略文件

下面的示例共用 [model.conf](examples/model.conf) 和 [policy.csv](examples/policy.csv)。将它们复制到运行程序的工作目录，命名为 `model.conf`、`policy.csv`。

模型使用角色继承、路径匹配和动作匹配，只有匹配 allow 策略才允许访问：

```ini
[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act
[role_definition]
g = _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && r.act == p.act
```

示例策略：

```csv
p, editor, blog, create
p, editor, blog, delete
p, editor, /blog, POST
p, editor, /blog/*, DELETE
g, alice, editor
g, editor, admin
```

`alice` 的直接角色是 `editor`；`editor` 继承 `admin`。资源授权会按模型处理角色继承，`GetRolesForUser("alice")` 只返回直接角色 `editor`。

## 文件 Adapter

准备好上述文件后，以下程序可直接运行：

```go
package main

import (
    "fmt"
    "log"

    "github.com/apus-run/gala/components/authz"
)

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    az, err := authz.New()
    if err != nil {
        return err
    }

    allowed, err := az.Authorize("alice", "blog", "create")
    if err != nil {
        return err
    }
    roles, err := az.GetRolesForUser("alice")
    if err != nil {
        return err
    }
    fmt.Println("allowed:", allowed)
    fmt.Println("roles:", roles)
    return nil
}
```

输出：

```text
allowed: true
roles: [editor]
```

自定义文件位置时使用 `WithModelFilePath` 和 `WithPolicyAdapter`：

```go
az, err := authz.New(
    authz.WithModelFilePath("./config/model.conf"),
    authz.WithPolicyAdapter(fileadapter.NewAdapter("./config/policy.csv")),
)
```

`fileadapter` 的导入路径为 `github.com/casbin/casbin/v3/persist/file-adapter`。

## GORM Adapter

以下示例使用 SQLite；其它数据库只需替换 GORM 驱动。示例程序先显式创建策略表，再构造授权器、写入策略并通过 `LoadPolicy` 刷新；生产环境可通过应用的迁移流程提前建表。NewAuthz 为 Adapter 创建独立 GORM session，清除查询条件并使用长期 Context，同时复用连接池。传入的 db 应为基础设施层根实例，不能是请求事务；NewDB 不会把事务恢复为根连接池。工作目录仍需准备 `model.conf`。

```sh
go get github.com/glebarez/sqlite
```

```go
package main

import (
    "fmt"
    "log"

    "github.com/apus-run/gala/components/authz"
    gormadapter "github.com/apus-run/gala/components/authz/adapter/gorm"
    "github.com/glebarez/sqlite"
    "gorm.io/gorm"
)

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    db, err := gorm.Open(sqlite.Open("policy.db"), &gorm.Config{})
    if err != nil {
        return err
    }
    sqlDB, err := db.DB()
    if err != nil {
        return err
    }
    defer sqlDB.Close()

    if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
        return err
    }
    az, err := authz.NewAuthz(db, authz.WithModelFilePath("./model.conf"))
    if err != nil {
        return err
    }

    rule := gormadapter.CasbinRule{
        Ptype: "p", V0: "alice", V1: "blog", V2: "create",
    }
    if err := db.Where(&rule).FirstOrCreate(&rule).Error; err != nil {
        return err
    }
    if err := az.LoadPolicy(); err != nil {
        return err
    }
    allowed, err := az.Enforce("alice", "blog", "create")
    if err != nil {
        return err
    }
    fmt.Println("allowed:", allowed)
    return nil
}
```

输出 `allowed: true`。组件不拥有数据库连接池或后台任务；应用先等待请求和策略同步任务结束，再关闭数据库。

如果应用已经创建了 GORM Adapter，也可以通过 `New(WithPolicyAdapter(adapter), ...)` 接入。

## 授权与角色查询

`Authorize` 是三字段模型的便捷调用；`Enforce` 支持与模型对应的任意参数数量。

```go
allowed, err := az.Authorize("alice", "blog", "create")
allowed, err = az.Enforce("alice", "blog", "create")

roles, err := az.GetRolesForUser("alice")
roles, err = az.GetRolesForUser("alice", "tenant-1")
```

域参数需要模型声明域角色关系，例如 `g = _, _, _`。无角色定义的模型查询角色时会返回错误。角色结果为独立切片，修改返回值不会改变授权器内的角色关系。

## 为什么关闭 AutoSave

构造时调用 `engine.EnableAutoSave(false)`，是为了保持 Authorizer 的职责边界：它读取策略并执行鉴权，策略写入由应用的管理服务或 Repository 完成。应用可以在写入路径统一处理管理权限、事务和审计，提交后调用 LoadPolicy 更新内存策略。

关闭 AutoSave 后，Enforcer 的 AddPolicy、RemovePolicy、UpdatePolicy 和角色规则修改仍可改变内存，但不会自动通过 Adapter 持久化。这些内存修改在后续 LoadPolicy 时可能被存储中的策略覆盖。

AutoSave 设置不影响 Enforce / Authorize，也不会阻止显式 SavePolicy；NewAuthz 另外通过 Adapter 配置关闭自动迁移。通过 New 注入 Adapter 时，是否迁移由该 Adapter 的创建过程决定。因此 AutoSave 是职责约束，不是数据库只读权限。当前 Authorizer 不公开底层 Enforcer 或策略管理方法；如果另一种封装要通过 Enforcer 直接维护数据库策略，则应选择开启 AutoSave。

## 策略重载与生命周期

Authorizer 负责初次加载、鉴权、重载与通知；不会自动启动 goroutine，也不关闭调用方的数据库。RunPolicyReload 阻塞运行，应用负责启动和等待。Redis Watcher 自己拥有订阅 goroutine，由应用 Close。

策略变更提交后，由应用的事件消费者或受管理同步任务调用：

```go
if err := az.LoadPolicy(); err != nil {
    return err
}
```

同一 Authorizer 的重载锁覆盖整次加载和应用过程，避免重载任务交错执行。普通鉴权请求使用已有内存策略，不调用 LoadPolicy。它只更新策略，不重新读取模型文件；模型变化后重新构造实例。

NewAuthz 使用 timedAdapter 为每次 GORM 策略读取创建独立超时 Context，默认 5 秒，初次加载和后续重载都受该超时约束。仅给 GORM session 设置 deadline 不能替代这一边界，因为普通 gorm-adapter.LoadPolicy 会使用 context.Background。

这个超时不包含模型解析、等待其它重载的时间或 Casbin 的角色图构建，也不控制文件 Adapter、任意自定义 Adapter。策略存储读取失败时保留已有内存策略，下一次重载使用新的超时 Context。重载错误包含阶段信息并保留原始错误链；失败重试与监控由应用的同步任务管理。

外部同步任务应统一调用当前实例的 LoadPolicy。应用退出时先停止接收请求和同步任务，等待它们结束，再关闭连接池。

NewAuthz 关闭 GORM Adapter 的自动迁移；策略表不存在时，构造会返回初始策略加载错误，不会创建表或修改调用方的迁移配置。需要使用 Adapter 默认迁移行为时，可自行创建 GORM Adapter 并通过 New(WithPolicyAdapter(adapter), ...) 注入。组件不强制嵌入模型或租户域。新建 Enforcer 关闭 AutoSave；策略由应用写入数据库或 Adapter，提交后通过 LoadPolicy 同步到授权器。

## 自定义组合选项

`OptionFunc` 可以组合已有选项。`NewOptions` 负责创建独立默认配置，`Apply` 按顺序原地应用，`Validate` 检查最终加载超时；模型与策略的加载错误在构造时返回。

```go
func WithPolicyFiles(modelPath, policyPath string) authz.Option {
    return authz.OptionFunc(func(o *authz.Options) {
        o.Apply(
            authz.WithModelFilePath(modelPath),
            authz.WithPolicyAdapter(fileadapter.NewAdapter(policyPath)),
        )
    })
}
```

Authorizer 保存初始化完成的 Enforcer、重载锁、Watcher 和新鲜度状态，不保留构造用的 Options。构造后修改保留的 Options 不会改变授权器。显式传入的 Adapter 仍由调用方管理。选项应为有效值，配置组装期间不应并发修改同一个 Options。

## 多实例同步与撤权期限

PRD、Midway 对照与消融证据见 [prd-authz-upgrade.md](prd-authz-upgrade.md)。

- `SyncPolicy` 必须在策略写入提交之后调用。它独立尝试本地重载和锁外通知，用 `errors.Join` 返回两侧错误；本地失败也会尝试通知健康实例。成功只表示本地加载与发布没有报错，不代表所有实例已经同步。
- `WithWatcher` 要求订阅已经就绪。构造时直接注册回调并补载一次，覆盖初次加载到注册之间的变更。不要再次修改 Watcher 回调，也不要在多个 Authorizer 之间共享同一 Watcher。构造失败时应用仍须关闭该 Watcher。
- Pub/Sub 会丢消息；每个实例都应运行 `RunPolicyReload`。重载失败保留旧策略并在下一周期重试；错误处理函数必须及时返回、并发安全，不同步重入重载、通知或关闭。
- 有撤权时间要求时启用 `WithMaxPolicyAge`。年龄大于等于配置值时返回 `ErrPolicyStale`，调用方必须拒绝请求；失败不会延长有效期。成功加载从读取开始计算年龄；若加载耗时已不小于最大年龄，且其后没有更新的成功加载，下一次鉴权即拒绝。RunPolicyReload 只校验 ticker 间隔小于最大年龄，不检查单次加载耗时。默认 0 仅用于兼容，不保证陈旧权限的寿命。
- 读取必须指向权威存储。该机制不能识别成功读到的滞后副本，也不撤销已进入业务 Handler 的操作。严格业务写权限仍应由事务约束。

```go
watcher, err := redisadapter.NewWatcher(ctx, client, "authz:tenant-a:changed")
if err != nil { return err }
defer watcher.Close()
adapter, err := redisadapter.NewAdapter(&redisadapter.Config{
    Client: client, Key: "authz:tenant-a:rules",
})
if err != nil { return err }
az, err := authz.New(
    authz.WithModelFilePath("./model.conf"),
    authz.WithPolicyAdapter(adapter),
    authz.WithWatcher(watcher),
    authz.WithWatcherErrorHandler(func(err error) { log.Print(err) }),
    authz.WithMaxPolicyAge(30*time.Second),
)
if err != nil { return err }
runCtx, cancel := context.WithCancel(ctx)
defer cancel()
results := make(chan error, 2)
go func() {
    results <- az.RunPolicyReload(runCtx, 5*time.Second,
        func(err error) { log.Print(err) })
}()
// serve 收到取消后须停止接收并排空请求、写入及 SyncPolicy，再返回。
go func() { results <- serve(runCtx, az) }()
first := <-results // 运行期间观察任一任务退出
cancel()
second := <-results // 只等待另一个任务一次，不能再 defer 读取
var failures []error
for _, err := range []error{first, second} {
    if err != nil && !errors.Is(err, context.Canceled) {
        failures = append(failures, err)
    }
}
return errors.Join(failures...)
```

`redisadapter` 的导入路径是 `github.com/apus-run/gala/components/authz/adapter/redis`。Redis 客户端必须配置有限读写/拨号超时；`persist.Adapter.LoadPolicy` 没有 Context，因此取消周期任务不能中断已开始的加载。

不需要快速通知时，省略 Watcher，仅保留周期重载与最大策略年龄即可。NewAuthz 使用本地 `github.com/apus-run/gala/components/authz/adapter/gorm`；NewRedis 是兼容便利入口，因此当前模块仍包含 GORM/Redis 依赖。新集成优先使用显式 Adapter 注入，不继续扩充存储专用构造函数。

可编译且带生命周期测试的完整示例见 [examples/supervision](examples/supervision/supervision.go)。本地 GORM 模块来源于上游 v3.41.0，保留上游许可证和版本记录；Casbin 依赖统一为 v3.10.0。

Redis Adapter 与 `NewRedis` 仅要求 `redis.Cmdable`；命令能力不包含资源所有权。Watcher 使用 `redisadapter.WatcherClient`，只要求 `Publish` 和 `Subscribe`。注入客户端由应用关闭，自建 Adapter 客户端通过单独保存的关闭函数释放。

## Docker 集成验证

```sh
# 从仓库根目录执行；需要本地已存在 Redis/MySQL/PostgreSQL 镜像。
python3 components/authz/integration/run.py
```

已通过 Redis 7.4.11 单机/三主节点 Cluster、MySQL 8.4.11 和 PostgreSQL 16.15 的 race 集成测试。验证完整快照、真实 ACL 失败、订阅重连、周期恢复、过期拒绝、事务可见性及读取超时。脚本使用隔离容器和随机回环端口，结束后自动清理。详见 [环境、覆盖范围与结果日志](integration/README.md)。
