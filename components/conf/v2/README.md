# Gala Conf v2

`conf.Open(ctx, defaults, sources...)` 同步加载按优先级排列的来源，返回一个持有只读快照的配置实例。后面的来源覆盖前面的来源。文件和命令行都缺席的键采用结构体默认值。

```go
type App struct {
    Port int `conf:"port"`
}

cfg, err := conf.Open(ctx, App{Port: 8080},
    file.New("app.json", file.JSON),
    env.New(map[string]string{"APP_PORT": "port"}),
)
if err != nil {
    return err
}
port := cfg.Current().Port
```

`Current()` 可并发读取。读者可在重载之后继续使用手里的旧快照。Open 复制可绑定字段中的默认集合与指针，之后可以修改原始默认值。忽略字段和自定义类型的私有引用仍须保持只读，所有已发布值均为只读。`Reload(ctx)` 从默认值和全部来源重新构建候选，等待重载锁时可以取消。失败的调用不会发布。业务类型可以实现只读的 `Validate() error`。Source.Load、Merge、Resolve 和 Validate 都在写事务内执行；这些回调可以读取上一版快照，禁止同步重入同一实例的 Reload、Load、Set、Delete 或 MergeAt。需要追加写入的业务逻辑应在发布后的订阅消费者中执行。

字段名区分大小写，可通过 `conf:"name"` 映射或 `conf:"-"` 忽略。对象递归覆盖，列表整体替换。未知键、显式 null、不支持的字段类型和数值溢出会产生错误。来源输入限深 128，每批最多展开 65,536 个节点。文件 JSON 保留数字文本，绑定到 int64、uint64 时按位宽精确解析。浮点目标使用对应精度舍入。固定长度数组要求长度匹配，导出的嵌入字段按字段名或标签作为嵌套键。结构体可包含 `map[string]any`。文本字段支持 `encoding.TextUnmarshaler`，例如 `slog.Level`、`time.Time` 和 `netip.Addr`。文本解码使用新的零值接收者。方法返回错误或因嵌入指针为空而 panic 时，绑定返回 StageBind 和安全字段路径，保留上次发布的快照。

`*conf.Error` 提供 Stage、来源序号 Source、安全字段路径 Path 和可由 errors.Is/As 检查的 Err。使用 file.JSON 时，可通过 errors.As 取得 *file.SyntaxError 的行号和字节列号（从 1 开始）。未知键在能唯一匹配时提示一个静态字段名。自定义来源须返回可安全展示的错误文本。业务校验失败使用固定文案；Validate 返回的错误可通过 Unwrap 取得。Validate 触发 panic 时返回 StageValidate，错误链只保留固定原因。

业务结构固定时读 `cfg.Current()`。同时需要动态查询时，先取一次 `s := cfg.Snapshot()`：`s.Value` 和 `s.Tree` 属于同一次发布。树保存解析后的显式键，结构体默认值只体现在 `s.Value`。例如缺席的端口可以让结构体取默认 8080，而 `s.Exists("server", "port")` 仍为 false。无预定义结构的程序可以直接用 `OpenTree`。

```go
cfg, err := conf.OpenTree(ctx, file.New("app.json", file.JSON))
if err != nil {
    return err
}
s := cfg.Snapshot()
port, err := conf.Get[int](s.Tree, "server", "port")
if err != nil {
    return err // errors.Is(err, conf.ErrNotFound) 可区分缺席
}
fmt.Println(port, s.Keys("server"))
```

路径参数逐段传递。`Lookup("a.b")` 查询字面键名，`Lookup("a", "b")` 查询嵌套键，列表元素用 `"0"` 等下标。空路径选根对象。Lookup 返回私有副本；Cut 返回固定在该版本上的子树；Decode 可读结构体、切片、映射和标量，失败保留调用者的目标值。使用 `json.Marshal(s.Tree)` 或 `s.Marshal(yaml.Marshal)` 导出配置，导出内容可能含密钥。直接用 fmt 的值格式（如 %v、%#v、%d）或 slog 记录 Tree、Snapshot、Change 时会脱敏。%T 和 %p 由 fmt 自行处理，可能显示类型、地址或格式诊断。取出的类型化 Value、Lookup 结果及导出的字节由应用自行决定如何记录。

首个快照的 Revision 为 1。成功校验后，解析树未变就保留 Current 指针和 Revision，新的来源列表和未解析输入仍会提交。

使用 `initial, changes, cancel := cfg.SubscribeSnapshot("server", "port")` 同时取得初始快照和订阅。先应用 initial，再消费 changes，事件总是晚于 initial。仅关心后续变化时使用 `cfg.Subscribe(...)`。业务代码在自己的 goroutine 中消费事件。`change.Before.Lookup(...)` 和 `change.After.Lookup(...)` 分别读取变化前后的值和存在性。订阅比较解析后的树值，同类型 NaN 视为相等（含嵌套集合），正零与负零之间的变化会发布并通知。被高层遮盖的配置变化和失败事务都不会发信号。慢消费者收到的 Before 来自最早的未读事件，After 是最新的状态。中途改动后又恢复时，两端的值可以相同。使用完毕调用取消函数。

**运行时编辑只作用于当前版本。** `Set(ctx, value, path...)`、`Delete(ctx, path...)`、`MergeAt(ctx, subtree, path...)` 直接修改候选，重新解析、绑定、校验并发布。下一次成功的 Reload 或 Load 会从来源重建并清除临时编辑；失败时保留当前版本。Load 可以事务性追加高优先级来源。

需要跨字段引用或自定义合并时，使用 `conf.OpenWith(ctx, defaults, conf.Options{Resolve: conf.Expand, Merge: conf.StrictMerge}, sources...)`。动态模式的 defaults 传 `conf.Tree{}`。Expand 支持 `${server.port}` 和 `${key:fallback}`，也解析底层类型为 string 的命名字符串（json.Number 保留为数值），对完整标量引用保留其类型，拼接文本时转换为字符串。`$${` 生成字面量 `${`。StrictMerge 比较重叠键的具体类型。env/flagset 的字符串覆盖 JSON 数字或 YAML 整数会失败。数值文件配合文本覆盖时，采用默认合并，让类型化绑定检查格式和范围。两个选项可独立启用。[可运行示例](tree_test.go)

| 来源 | 入口 | 行为 |
|---|---|---|
| 文件 | `file.New(path, dec)`、`file.FS(fsys, name, dec)` | 每批读一次；显式注入 file.JSON 或 yaml.Unmarshal 等解码函数 |
| 本地文件监视 | 独立模块 `sources/filewatch` 的 `filewatch.New(path, dec)` | 读取同一文件；`src.Watch(ctx)` 发送需要重载的信号 |
| 环境 | `env.New(bindings)`、`env.NewWithLookup(bindings, lookup)`、`env.Scan(environ, mapping)` | 显式映射点分路径，每次刷新，空串视为缺席 |
| 命令行 | `flagset.New(flags)` | 只读取 Parse 后显式设置的 flag |
| etcd、Consul | `New(client, key, dec)`、`NewPrefix(client, prefix)` | 分别在独立 Go 模块中提供单键文档和目录树来源 |

可选覆盖文件可用 `conf.Optional(file.New("app.local.json", file.JSON))`。只有缺失会变成空层，权限、解码及取消错误仍会中止事务。需要限制文件输入时，使用 `src.WithMaxBytes(1 << 20)` 返回的新来源，超限可用 `errors.Is(err, file.ErrTooLarge)` 判断。零值沿用完整读取。可选的监视来源将包装后的来源传给 Open，将原始 Watcher 传给 Run。

本地文件需要热更新时，用 `filewatch.New` 创建来源。在 Open 和 Watch 中使用同一实例：

```go
src := filewatch.New("app.json", file.JSON)
cfg, err := conf.Open(ctx, App{Port: 8080}, src, env.New(bindings))
if err != nil {
    return err
}
return conf.Run(ctx, cfg, conf.RunOptions{
    OnError: func(err error) { slog.Warn("配置重载失败", "err", err) },
}, src)
```

`conf.Run` 接受多个 Watcher，在最后一个信号后默认等待 75ms 再全量重读。指定 OnError 后，即使没有新事件也会重试。等待上限从 1 秒指数增长至 30 秒，每次在上限的一半到上限之间随机等待。OnError 为空时，首次重载失败就返回。取消 ctx 会结束运行并返回 ctx.Err()。所有 Watcher 关闭时，有待处理的变更会再尝试一次。写入方需要保证文件完整时，应原子替换文件。

父目录须在注册时存在，目标文件可以暂时缺失。注册成功后会先发一次信号，覆盖 Open 与 Watch 之间的更新。按所在文件系统的名称查找规则识别目标文件，直接写入、原子替换、删除和重建均会触发信号；运行中替换父目录后，重新注册成功也会补发。支持 Kubernetes 投影卷的 `..data` 原子切换。对于投影路径，每秒还会核对一次文件身份，以覆盖文件系统合并掉目录事件的情况。其他目录中独立写入的符号链接目标仍需额外触发。ctx 取消后监听器释放系统资源并关闭通道。

远端来源的 `src.Watch(ctx)` 在订阅就绪、恢复和内容变化时发送可合并的信号。远端订阅重连使用可取消的指数退避和随机抖动。ctx 管理订阅生命周期，SDK 客户端由应用关闭。Consul 使用一致读；启用 ACL 的前缀读取要求服务端支持过滤响应头、有效非匿名 token，并完整透传查询元数据。已标记过滤的结果返回权限错误。

从选项版迁移时，用结构体默认值及来源列表调用 Open，字段标签改为 conf。应用持有传给 Run 或 Watch 的 context，文件解码函数通过构造参数传入。详细契约、迁移表和验收条件见[规格](tasks/spec.md)。可运行的本地配方见[示例](example_test.go)，远端信号消费见[重试示例](sources/etcd/example_test.go)。


构建工具链最低版本为 Go 1.26.11。下一候选须先发布核心 `components/conf/v2.0.0-rc.2`，再发布 etcd、Consul 和 filewatch 适配器。适配器的 go.mod 引用该核心版本。发布前可从核心目录运行 `testdata/ci/check.sh` 和 `testdata/ci/integration.sh` 验证同一工作树；脚本在临时目录中测试适配器与当前核心。对外安装验收使用独立消费者、公共 Go 代理和 `GOWORK=off`。
