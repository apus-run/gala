# conf/v2

多来源、强类型配置。每批按顺序合并来源，严格绑定并调用业务校验；全部成功后原子发布，任何失败都保留上一份有效配置。

## 加载与重载

```go
import (
    "context"

    conf "github.com/apus-run/gala/components/conf/v2"
    "github.com/apus-run/gala/components/conf/v2/sources/env"
    "github.com/apus-run/gala/components/conf/v2/sources/file"
    _ "github.com/apus-run/gala/pkg/encoding/yaml"
)

type App struct {
    Port int `mapstructure:"port"`
}

func Load(ctx context.Context) (*conf.Config[App], error) {
    return conf.Open[App](ctx,
        conf.WithSources(
            file.New("config.yaml"),
            env.New(map[string]string{"PORT": "port"}),
        ),
    )
}
```

`Open` 同步完成首次加载，失败返回 nil 实例。默认不启动后台任务；调用方持有实例后可以 `defer cfg.Close()`。

| 操作 | 读取与发布语义 |
|---|---|
| `Current()` | 无锁读取当前快照；不执行 I/O |
| `Load(ctx, sources...)` | 读取新增来源，叠加到上次成功读取的各层快照之上；成功后注册新增来源，优先级最高 |
| `Reload(ctx)` | 从空配置开始，重新读取全部已注册来源；覆盖值消失后，下层值重新显现 |
| `Start(ctx)` | 启动来源监听和已配置的轮询；运行期间重复调用不会重复启动 |
| `Close()` | 取消进行中的读取与监听，等待后台协程退出，关闭订阅；幂等 |

Load 和 Reload 共用串行、可取消的执行入口。来源列表、各层缓存与最终配置一起提交。失败的 Load 不会留下来源、缓存或新增监听器。

每个业务操作读取一次 `snapshot := cfg.Current()` 并沿用它。快照及其嵌套集合只读；重载后旧快照仍有效。Config 使用后不能复制。Close 后 Current 仍可读，新的 Load/Reload/Start 返回错误。

## 自动更新与通知

```go
cfg, err := conf.Open[App](ctx,
    conf.WithSources(file.New("config.yaml")),
    conf.WithWatch(),
)
if err != nil {
    return err
}
defer cfg.Close()

events, unsubscribe := cfg.Subscribe()
defer unsubscribe()
for event := range events {
    if event.Err != nil {
        // 记录已脱敏的 event.Err.Error()；Current 仍为有效配置。
        continue
    }
    apply(event.Current)
}
```

只有 `WithWatch()` 或显式 Start 才启动后台任务。启动失败会清理已经打开的监听器。文件监听父目录，支持原子替换；etcd 使用 revision watch；Consul 使用 blocking query。它们仅报告变化，由核心重新读取全部来源，确保声明顺序持续决定优先级。

符号链接路径额外每秒补偿一次完整重载，覆盖操作系统遗漏的链接切换事件及链接目标目录之外的更新。复用现有监听协程，不增加协程；配置内容未变时不发送变化通知。普通路径仍由原生事件驱动；`WithPollInterval` 可额外指定周期轮询。

Watcher 失败会发出固定错误通知，每秒尝试重新建立监听；恢复后完整重载，补齐断连期间的变化。文件所在目录被重命名或删除也会触发重新建立监听。自动重载失败时保留旧值，每秒重试，成功后停止补偿重试，避免一次临时读取失败吞掉唯一的变更事件。持续无效的配置也会持续重试；通知仍只保留最新一条。

监听 ENV、内存 FS、自定义静态来源时，可添加 `WithPollInterval(time.Second)`。这种周期轮询默认关闭，配置成功后仍会持续执行；它与原生事件可以同时启用。

Start 的 ctx 取消只停止该轮监听，之后可以再次 Start。WithWatch 使用 Open 的 ctx 作为监听生命周期。Close 则关闭整个实例；它不会关闭借用的 etcd/Consul 客户端。Source.Load、Watcher.Next 与 Resolver 必须遵守 ctx；不响应取消的用户代码无法被强制终止。

Source.Load、Watch、Watcher.Next/Close、Merge、Resolver、Validate 等用户实现不得重入同一个 Config 的 Load/Reload/Start/Close；可以读取 Current。Observe 的选择器必须只读且及时返回。库不增加重入检测或强制终止机制。

`Subscribe` 不启动额外协程。成功且配置内容改变时发出快照事件，自动更新失败时发出错误事件；手动 Load/Reload 的错误由返回值交付。每个订阅只保留最新待处理事件，慢订阅者不会阻塞重载。这是状态通知，不是逐次可靠事件日志；错误事件也携带当前有效快照。

按字段观察使用类型安全的选择器：

```go
changes, stop := conf.Observe(ctx, cfg, func(app *App) int { return app.Port })
defer stop()
for change := range changes {
    updatePort(change.Current)
}
```

Observe 只在选中值改变时通知，并正确处理合并通知后的最终状态。选择器必须只读、及时返回；stop 等待该观察协程退出。

## 来源与格式组合

核心只要求 `Source.Load(context.Context) (map[string]any, error)`。可使用 `conf.SourceFunc` 适配函数；来源无须实现 Watch 或 Close。来源实现统一位于 `sources/`，它是组织目录，不是注册中心。

- `file.New(path)`：显式 OS 路径，按扩展名选择已注册 codec；`.yml` 等同 `.yaml`。
- `file.NewFS(fsys, name)`：指定 io/fs 文件；name 须满足 fs.ValidPath 且不是 `.`。
- `env.New(bindings)`：显式“变量名 → 点分字段路径”；未设置、空串不覆盖下层。NewWithLookup 可注入取值函数。
- 独立 `sources/etcd`：`etcd.New(client, key, "yaml")`。
- 独立 `sources/consul`：`consul.New(client.KV(), key, "toml")`。
- codec 分别导入 `pkg/encoding/json`、`yaml`、`toml`；核心不自动导入格式实现。

需要实例级解码器或自定义来源时，使用 `sources/document`：

```go
source := document.New(
    document.ReaderFunc(func(ctx context.Context) ([]byte, error) {
        return fetchDocument(ctx)
    }),
    myCodec, // 实现 encoding.Codec，不需要全局注册。
)
cfg, err := conf.Open[App](ctx, conf.WithSources(source))
```

文件与远端来源也实现 ReadBytes：`document.New(file.New("config.data"), myCodec)` 可以显式指定格式；远端可用 `etcd.NewReader(client, key)` 或 `consul.NewReader(client.KV(), key)`。document 会转发读取器的可选监听能力。

自定义监听来源额外实现 `Watch(ctx) (conf.Watcher, error)`；Watcher 只有 Next(ctx) 和 Close。包装器可返回 ErrWatchUnsupported 表示当前读取器不支持监听。

## 合并与解析扩展

默认对象递归合并、列表整体替换；后声明的来源优先。若业务确实需要不同策略，可使用：

```go
conf.WithMerge(func(dst, src map[string]any) error {
    // 将 src 合并进 dst，例如按业务规则合并特定列表。
    return nil
})
conf.WithResolver(func(ctx context.Context, candidate map[string]any) error {
    // 在全部来源合并后处理占位符、迁移字段等。
    return ctx.Err()
})
```

两个扩展均在候选副本上运行，不得在返回后并发改写候选。输入对象统一为 `map[string]any`，列表和数组统一为 `[]any`，标量保留其值与类型；扩展无需按来源区分集合类型。来源输入先校验复制，扩展完成后再次检查并复制，随后严格绑定与 Validate。扩展失败整体回滚，错误文本不直接回显扩展返回的消息。没有扩展时沿用默认规则，不需要创建策略对象。

## 数据边界与诊断

目标类型须为结构体值。未知键报错，字段名区分大小写；支持字符串、有符号整数、布尔值及带单位的 time.Duration 字符串。浮点数、无符号数和 interface 字段暂不支持实际绑定值。

来源只接受标量、字符串键 map、slice 和 array；禁止结构体值、指针、显式 null 和循环引用。字符串键包含命名字符串类型，进入核心时统一转成普通字符串键，所有对象遵循相同的递归合并规则。从根对象到叶子的集合边数最多 128。业务结构体使用嵌套 map 表示；TOML 原生日期时间等 codec 返回的结构体值也会被拒绝，可使用带引号的字符串。来源不得在库复制期间并发改写返回数据；跨多个 Config 共用来源时自行保证线程安全。

若 `*App` 实现 Validate() error，绑定后、发布前调用一次。校验只读，不得保留候选引用供后续改写。

Open/Load/Reload/Start 返回的错误均为 `*conf.Error`。固定原因示例：

```text
conf: decode failed (source 0): codec not registered
conf: decode failed (source 0): document is empty
conf: bind failed at server.port: value out of range
conf: resolve failed: resolver failed
```

绑定路径只来自已匹配的可信 schema 字段。未知键、动态集合元素、squash/remain 和含控制字符的标签省略路径。业务校验错误链保留供 errors.Is/As 判定，不能将其展开文本视作已脱敏。借入 SDK、codec、合并与解析扩展的原始错误消息均不展示。

## 本地验证

核心、`sources/etcd`、`sources/consul`、`integration`、`pkg/encoding/toml` 有独立 go.mod，须分别执行适用检查：

```sh
GOTOOLCHAIN=go1.27.2 GOWORK=off go build ./...
GOTOOLCHAIN=go1.27.2 GOWORK=off go vet ./...
GOTOOLCHAIN=go1.27.2 GOWORK=off go test -race ./...
```

真实后端使用专用临时 key，测试结束后删除：

```sh
cd integration
docker compose up -d
# 等待两个后端就绪。
CONF_TEST_ETCD=http://127.0.0.1:52379 \
CONF_TEST_CONSUL=127.0.0.1:58500 \
GOTOOLCHAIN=go1.27.2 GOWORK=off go test -race -v ./...
docker compose down -v
```

未设置测试地址时，真实后端用例显式跳过。独立消费者还验证最小 JSON 文件配置不引入远端 SDK 或 YAML/TOML 实现。

v2 尚未发布：旧的 `Open(ctx, source...)` 调用迁移为 `Open(ctx, WithSources(source...))`。本地 replace 仅供候选开发；发布仍需独立模块版本、无 replace 代理消费者及模块 zip 验收。

对抗性审查与核心机制消融实验见 [AUDIT.md](AUDIT.md)。可在本目录运行 `python3 scripts/ablate.py --output /tmp/conf-ablation.json` 复现；实验使用 Go overlay，不修改源码或暂存区。
