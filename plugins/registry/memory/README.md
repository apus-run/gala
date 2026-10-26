# Memory registry

进程内的服务注册与发现实现，适合测试、本地开发和单进程应用。
同一个 `*Registry` 同时实现 `registry.Registry` 和 `registry.Discovery`，
共用实例数据、订阅和生命周期。没有网络请求、持久化、TTL 或健康检查；
不同 `New()` 创建的注册中心彼此独立。

## 使用

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/apus-run/gala/plugins/registry/memory"
    "github.com/apus-run/gala/registry"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    r := memory.New(memory.WithBufferSize(64))
    defer r.Close()

    instance := &registry.ServiceInstance{
        Name: "orders",
        Endpoints: []string{"grpc://127.0.0.1:9000"},
    }
    if err := r.Register(ctx, instance); err != nil {
        log.Fatal(err)
    }

    events, err := r.Subscribe(ctx, "orders")
    if err != nil {
        log.Fatal(err)
    }
    added := <-events // 首先回放现有实例。
    fmt.Println(added.Type, added.Instance.ID)

    if err := r.Deregister(ctx, instance); err != nil {
        log.Fatal(err)
    }
    removed := <-events
    fmt.Println(removed.Type, removed.Instance.ID)
}
```

## 行为约定

- `Register` 保存实例的深拷贝；ID 为空时生成并写回调用方的 `instance.ID`。
  相同 Name 和 ID 再次注册会覆盖旧值，并发送 Add 事件。
- Name 必须非空，Name 和 ID 均不能包含 `/`。`Deregister` 要求 ID 非空；
  注销不存在的实例是无操作。
- `GetService` 返回按 ID 排序的实例副本，未知服务返回空切片。
- `Subscribe` 在同一把锁内取得快照并建立订阅，先回放现有实例的 Add，
  再发送增量事件。Add 表示按 ID 新增或覆盖，Delete 表示按 ID 移除。
- 事件中的实例可能由多个订阅者共享。修改前调用 `Clone()`。
- 每个订阅的 channel 容量为初始实例数加配置缓冲量。`WithBufferSize`
  默认 64，非正值被忽略。缓冲满时关闭该订阅，不阻塞其他订阅者。
- 取消订阅 context 会关闭 channel；已缓冲的事件仍可读出。
  调用方不能关闭 channel。因消费过慢而关闭时，应清空本地状态并重新订阅。
- `Close` 幂等，关闭全部订阅并清空实例，不逐个发送 Delete。
  关闭后，参数和 context 有效的注册、查询、订阅返回 `registry.ErrClosed`；
  注销仍是无操作。注册与发现共用生命周期，不能分别关闭。
- 入口已取消的 context 会返回对应错误。注册时对调用方实例的 ID 回写
  不受内部锁保护，调用方应避免并发读写同一个实例。

实现和配置集中在 `memory.go`，共用一份状态与锁，无需拆分注册与发现对象。
