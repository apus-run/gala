# etcd 注册与发现

直接使用 etcd SDK。注册和发现拥有独立生命周期，调用方负责创建、关闭 `*clientv3.Client`。

## 同时注册与发现

分别创建注册端和发现端：

```go
r, err := etcd.NewRegistry(client)
if err != nil {
    return err
}
defer r.Close()

instance := &registry.ServiceInstance{
    Name: "order",
    Endpoints: []string{"grpc://127.0.0.1:9000"},
}
if err := r.Register(ctx, instance); err != nil {
    return err
}
// instance.ID 在生成时回填；请求失败后重试同一实例会沿用该 ID。
d, err := etcd.NewDiscovery(client)
if err != nil {
    return err
}
defer d.Close()
instances, err := d.GetService(ctx, "order")
```

## 按需使用

- `NewRegistry(client)`：只注册，创建一个共享租约。同一个 Registry 注册的实例共用该租约。
- `NewDiscovery(client)`：只发现，不申请租约；构造时不发起网络请求。

## 生命周期与故障

- 注册端关闭或租约丢失，不影响发现端的查询和订阅；关闭发现端也不会撤销注册租约。
- 租约丢失后，注册端返回 `registry.ErrClosed`。调用方需要重新创建 Registry 并重新注册实例；目前不自动重注册。
- `Close` 支持并发及重复调用。注册端先取消进行中的请求、停止 keepalive，再用独立的 5 秒超时撤销租约；租约已不存在视为成功。
- SDK client 由调用方持有，应在注册端和发现端都关闭后再关闭它。

## 订阅

`Subscribe(ctx, name)` 先回放快照，再从快照版本的下一版本监听，避免衔接期间漏事件。只有收到 watch 创建确认后才返回成功。

订阅的增量缓冲为 64，初始快照另预留空间。上下文取消、发现端关闭、监听错误、无效事件或慢消费者都会结束订阅并关闭 channel。除主动取消或关闭外，调用方应丢弃旧状态，重新订阅并从快照重建。

监听错误、无效事件和慢消费者关闭通过标准库 `slog` 记录。

## 测试

```sh
go test -race -timeout 60s ./...
```

测试通过 etcd SDK 的 KV、Watcher、Lease 接口注入故障，覆盖角色隔离、快照衔接、取消、并发关闭和租约清理；不需要运行 etcd 集群。

命名调整：原 `NewRegistrar` 改为 `NewRegistry`，原组合入口移除。
原先通过 Registry 调用的 `GetService`、`Subscribe` 改为使用独立的 Discovery。
