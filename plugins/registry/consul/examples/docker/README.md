# consul Docker 示例

Docker Compose 启动单节点 consul，本机 Go 程序演示注册、发现和注销。
使用当前模块代码，不需要额外 go.mod。需要 Go 1.27、Docker 和支持 `--wait` 的 Docker Compose。

## 运行

在仓库根目录执行：

```sh
cd plugins/registry/consul
docker compose -f examples/docker/compose.yaml up -d --wait --wait-timeout 90
go run ./examples/docker
```

程序为每次运行生成独立的服务名，依次执行：

1. 创建独立的 Registry 与 Discovery。
2. 注册 v1 实例，订阅并接收初始 Add 回放。
3. 查询并校验实例快照。
4. 更新到 v2，等待 Add 更新事件。
5. 注销实例，等待 Delete 事件，然后关闭注册与发现对象。

预期输出（服务名和实例 ID 每次不同）：

```text
registered name=gala-example-... id=...
event: add version=v1
query: 1 matching instance
event: add version=v2
event: delete version=v2
OK: register, replay, query, update, deregister
```

注册的 `http://127.0.0.1:8080` 仅为演示元数据，示例不启动业务 HTTP 服务。
程序总超时默认 30 秒，任何请求或事件校验失败都会以非零状态退出；退出时尝试清理注册实例。

```sh
go run ./examples/docker -address http://127.0.0.1:8500 -timeout 60s
```

`-address` 可连接其他实例；若修改本地 Compose 的宿主机端口，需同步调整该参数。

## 检查与清理

```sh
docker compose -f examples/docker/compose.yaml ps
docker compose -f examples/docker/compose.yaml logs consul
docker compose -f examples/docker/compose.yaml down --volumes
```

演示数据不持久化，清理命令删除此示例的容器及卷。配置仅绑定本机回环地址，
使用无 TLS、无鉴权的本地演示配置，不作为生产部署方案。

配置参考 [consul 官方 Docker 文档](https://developer.hashicorp.com/consul/docs/deploy/server/docker)。

Consul UI：<http://127.0.0.1:8500>。示例结束即注销实例，因此 UI 中不会保留该实例。
