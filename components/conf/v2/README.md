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

`Current()` 可并发读取。读者可在重载之后继续使用手里的旧快照。传给 Open 的默认 map、slice、指针和所有已发布值均须视为只读。`Reload(ctx)` 从默认值和全部来源重新构建候选，等待重载锁时可以取消。失败的调用不会发布。业务类型可以实现只读的 `Validate() error`。Source.Load、Merge、Resolve 和 Validate 都在写事务内执行；这些回调可以读取上一版快照，禁止同步重入同一实例的 Reload、Load、Set、Delete 或 MergeAt。需要追加写入的业务逻辑应在发布后的订阅消费者中执行。

字段名区分大小写，可通过 `conf:"name"` 映射或 `conf:"-"` 忽略。对象递归覆盖，列表整体替换。未知键、显式 null、不支持的字段类型和数值溢出会产生错误。来源输入限深 128，每批最多展开 65,536 个节点。文件 JSON 保留数字文本，绑定到 int64、uint64 时按位宽精确解析。浮点目标使用对应精度舍入。

`*conf.Error` 提供 Stage、来源序号 Source、安全字段路径 Path 和可由 errors.Is/As 检查的 Err。自定义来源须返回可安全展示的错误文本。业务校验失败使用固定文案，原始业务原因可通过 Unwrap 取得。

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

路径参数逐段传递。`Lookup("a.b")` 查询字面键名，`Lookup("a", "b")` 查询嵌套键，列表元素用 `"0"` 等下标。空路径选根对象。Lookup 返回私有副本；Cut 返回固定在该版本上的子树；Decode 可读结构体、切片、映射和标量，失败保留调用者的目标值。使用 `json.Marshal(s.Tree)` 或 `s.Marshal(yaml.Marshal)` 导出配置，导出内容可能含密钥。

使用 `initial, changes, cancel := cfg.SubscribeSnapshot("server", "port")` 同时取得初始快照和订阅。先应用 initial，再消费 changes，事件总是晚于 initial。仅关心后续变化时使用 `cfg.Subscribe(...)`。业务代码在自己的 goroutine 中消费事件。`change.Before.Lookup(...)` 和 `change.After.Lookup(...)` 分别读取变化前后的值和存在性。订阅比较解析后的树值，同类型 NaN 视为相等（含嵌套集合）。被高层遮盖的配置变化和失败事务都不会发信号。慢消费者只收到最近一次待处理的变化。使用完毕调用取消函数。

**运行时编辑只作用于当前版本。** `Set(ctx, value, path...)`、`Delete(ctx, path...)`、`MergeAt(ctx, subtree, path...)` 直接修改候选，重新解析、绑定、校验并发布。下一次成功的 Reload 或 Load 会从来源重建并清除临时编辑；失败时保留当前版本。Load 可以事务性追加高优先级来源。

需要跨字段引用或自定义合并时，使用 `conf.OpenWith(ctx, defaults, conf.Options{Resolve: conf.Expand, Merge: conf.StrictMerge}, sources...)`。动态模式的 defaults 传 `conf.Tree{}`。Expand 支持 `${server.port}` 和 `${key:fallback}`，也解析底层类型为 string 的命名字符串（json.Number 保留为数值），对完整标量引用保留其类型，拼接文本时转换为字符串。StrictMerge 对同名键的类型冲突报错。两个选项可独立启用。[可运行示例](tree_test.go)

| 来源 | 入口 | 行为 |
|---|---|---|
| 文件 | `file.New(path, dec)`、`file.FS(fsys, name, dec)` | 每批读一次；显式注入 file.JSON 或 yaml.Unmarshal 等解码函数 |
| 本地文件监视 | 独立模块 `sources/filewatch` 的 `filewatch.New(path, dec)` | 读取同一文件；`src.Watch(ctx)` 发送需要重载的信号 |
| 环境 | `env.New(bindings)`、`env.NewWithLookup(bindings, lookup)`、`env.Scan(environ, mapping)` | 显式映射点分路径，每次刷新，空串视为缺席 |
| 命令行 | `flagset.New(flags)` | 只读取 Parse 后显式设置的 flag |
| etcd、Consul | `New(client, key, dec)`、`NewPrefix(client, prefix)` | 分别在独立 Go 模块中提供单键文档和目录树来源 |

本地文件需要热更新时，用 `filewatch.New` 创建来源。在 Open 和 Watch 中使用同一实例：

```go
src := filewatch.New("app.json", file.JSON)
cfg, err := conf.Open(ctx, App{Port: 8080}, src, env.New(bindings))
if err != nil {
    return err
}
changes, err := src.Watch(ctx)
if err != nil {
    return err
}
for range changes {
    for ctx.Err() == nil {
        if err := cfg.Reload(ctx); err == nil {
            break
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(time.Second):
        }
    }
}
return ctx.Err()
```

父目录须在注册时存在，目标文件可以暂时缺失。注册成功后会先发一次信号，覆盖 Open 与 Watch 之间的更新。按所在文件系统的名称查找规则识别目标文件，直接写入、原子替换、删除和重建均会触发信号；运行中替换父目录后，重新注册成功也会补发。监听针对这条目录项，其他路径上的符号链接目标发生变化时需由应用另行触发。ctx 取消后监听器释放系统资源并关闭通道。

远端来源的 `src.Watch(ctx)` 在订阅就绪、恢复和内容变化时发送可合并的信号。应用收到信号后调用 Reload，失败时经过可取消的固定等待再重试。ctx 管理订阅生命周期，SDK 客户端由应用关闭。Consul 使用一致读；启用 ACL 的前缀读取要求服务端支持过滤响应头、有效非匿名 token，并完整透传查询元数据。已标记过滤的结果返回权限错误。

从选项版迁移时，用结构体默认值及来源列表调用 Open，字段标签改为 conf。应用持有监视 context 和重试循环，文件解码函数通过构造参数传入。详细契约、迁移表和验收条件见[规格](tasks/spec.md)。可运行的本地配方见[示例](example_test.go)，远端信号消费见[重试示例](sources/etcd/example_test.go)。


构建工具链最低版本为 Go 1.27.4。发布候选先发布核心 `components/conf/v2.0.0-rc.1`，再发布 etcd、Consul 和 filewatch 适配器。适配器的 go.mod 引用该核心版本。发布前可从核心目录运行 `testdata/ci/check.sh` 和 `testdata/ci/integration.sh` 验证同一工作树；脚本在临时目录中测试适配器与当前核心。对外安装验收使用独立消费者、公共 Go 代理和 `GOWORK=off`。
