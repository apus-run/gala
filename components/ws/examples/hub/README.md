# Hub 示例：在 gala ws 之上实现 IM/通知业务层

组件库 `ws` 只做传输层（升级、心跳超时、重连）；userID→连接注册表、订阅推送、
事件分发这些 IM 语义属于业务层。本示例是可直接照抄进业务仓库的参考实现：

- 多设备：一个 userID 多个连接，推送到达该用户所有设备
- 三维路由：`Push`（按用户）/ `PushTag`（按订阅 tag）/ `Broadcast`（全体）
- 读写分离：readLoop 独占读，writeLoop 独占写（gorilla 不允许并发写）
- 非阻塞推送：慢客户端缓冲满直接丢弃，不拖垮推送方
- 写超时 + 幂等关闭 + 优雅停机（SIGINT/SIGTERM 关闭所有连接）

## 运行

```bash
go run ./examples/hub
```

## 测试

```bash
# 终端1：连接并订阅（连接后会收到 {"event":"connected",...}）
wscat -c "ws://localhost:8080/ws?user_id=alice"
> {"event":"subscribe","tag":"case-123"}
> {"event":"ping"}

# 终端2：触发推送（tag > user_id > 全体）
curl "http://localhost:8080/push?user_id=alice&msg=hello"
curl "http://localhost:8080/push?tag=case-123&msg=new-progress"
curl "http://localhost:8080/push?msg=to-all"
```

## 上行协议

| event | 作用 |
|---|---|
| `subscribe` / `unsubscribe` | 订阅/退订 tag（`{"event":"subscribe","tag":"case-123"}`） |
| `ping` | 应用层心跳，服务端回 `pong` |

## 生产化要补的（示例刻意未含）

- 鉴权：`/ws` 处理器里从 cookie/header 完成鉴权（勿把 token 放 URL）
- 允许匿名连接、登录后换绑 userID：处理 login 事件时 `Unregister` + 重新 `Register`
- 重量级事件处理器：放进带并发上限的 goroutine 池
- 多实例部署：Hub 只在本进程内，跨实例推送需引入消息总线（Redis pub/sub、Kafka）
