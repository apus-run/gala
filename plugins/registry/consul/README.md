# Consul registry

通过 Consul Agent HTTP API 实现 `registry.Registry` 和 `registry.Discovery`。
独立 Go 模块，仅依赖 gala 与标准库，不增加 Consul SDK。

```go
r, err := consul.NewRegistry(consul.WithAddress("127.0.0.1:8500"))
if err != nil {
    return err
}
defer r.Close()

instance := &registry.ServiceInstance{
    Name: "orders",
    Endpoints: []string{"grpc://127.0.0.1:9000"},
}
if err := r.Register(ctx, instance); err != nil {
    return err
}
d, err := consul.NewDiscovery(consul.WithAddress("127.0.0.1:8500"))
if err != nil {
    return err
}
defer d.Close()
events, err := d.Subscribe(ctx, "orders")
if err != nil {
    return err
}
for event := range events {
    // Add 是按 ID 覆盖；Delete 是按 ID 移除。
    _ = event
}
```

只注册或只发现时，分别使用 `NewRegistry`、`NewDiscovery`。
两个角色独立创建和关闭，Registry 只负责注册，Discovery 只负责发现。
传入的 HTTP client 由调用者持有，插件不关闭它。

## 文件职责

| 文件 | 职责 |
| --- | --- |
| `registry.go` | 注册、注销、TTL 保活及生命周期协调 |
| `discovery.go` | 健康实例查询、阻塞查询、快照差异 |
| `client.go` | HTTP 请求、认证头、超时及状态错误 |
| `convert.go` | Consul 数据与 ServiceInstance 的转换 |
| `types.go` | 配置及默认值 |

没有额外的 client 接口、通用后端基类或可插拔策略。
契约测试复用 `registry/registrytest`，HTTP 行为通过本地测试服务器验证。

## 行为约定

- 默认地址为 `CONSUL_HTTP_ADDR`，未设置时使用 `http://127.0.0.1:8500`；
  ACL token 可通过 `CONSUL_HTTP_TOKEN` 或 `WithToken` 设置。
- 默认 TTL 为 15 秒，每 TTL/3 续期；持续 critical 一分钟后由 Consul 注销。
  Agent 丢失检查后，保活尝试重新注册。
- Agent service ID 使用 `name/id`，避免连字符拼接冲突。实例 ID 在整个逻辑服务内
  应唯一；发现相同 ID 时仅保留一个实例。
- `gala.` 元数据前缀保留给插件，携带 ID、版本和完整 endpoints；
  endpoints JSON 超过 512 字节会被拒绝。其他 Consul 校验错误直接返回。
- 首次订阅回放当前健康实例，然后按快照差异发送事件。网络或服务端故障会重试；
  恢复后收敛到最新状态，不保证回放两次查询之间的短暂变化。
- 慢消费者或不可恢复错误会关闭订阅，调用者应清空缓存并重新订阅。
- Close 取消并等待请求与后台任务，再用独立的 5 秒超时注销登记的实例。
  清理错误会返回；在无法连接 Agent 时，TTL 是最终失效保障。
- 阻塞查询处理索引回退及零索引，并设置含等待抖动余量的客户端超时。
  规则参见 [Consul blocking queries](https://developer.hashicorp.com/consul/api-docs/features/blocking)。

```sh
cd plugins/registry/consul
go test -race ./...
```

测试不要求运行 Consul；未覆盖真实集群、ACL 策略和 TLS 部署联调。

命名调整：原注册端构造函数统一为 `NewRegistry`，不再提供组合包装类型。
查询和订阅使用 `NewDiscovery` 返回的对象。

## Docker 示例

见 [Docker 注册与发现示例](examples/docker/README.md)，包含 Compose 配置与可运行的 Go 程序。
