# NSQ 适配器

使用官方 `github.com/nsqio/go-nsq v1.1.0`，传输原生 NSQ 消息体，不添加私有编码协议。

## 能力与配置

| 接口 / 配置 | 行为 |
| --- | --- |
| Send | PUB；有 DeferDuration 时使用 DPUB |
| SendBatch | 同 Topic 使用 MPUB；发送前验证整个批次，拒绝混合 Topic 和延迟批次 |
| SendAsync | 成功入 SDK 后回调一次；本地校验或入队错误直接返回，不调用回调 |
| Addr | nsqd TCP 地址，如 `127.0.0.1:4150`；不接受 lookupd 地址 |
| 多个 Addr | 生产者轮询选择节点；消费者连接全部节点 |
| ConsumerGroup | NSQ channel，组内共享消息，不同 channel 各收一份 |
| ConsumeGoroutineNums | SDK handler 并发数及 MaxInFlight，0 按 1 处理 |
| ConsumeTimeout | handler context 截止时间和 SDK MsgTimeout；0 不额外添加应用层截止时间 |
| ProduceTimeout | SDK 建连、读取、写入超时；遵守 SDK 的 100ms–5min I/O 配置范围 |
| ProducerGroup | SDK ClientID |
| SendResponse | NSQ 发布响应没有消息 ID/offset，返回空值；消费 MsgID 为 broker 的 16 字节 ID |

DPUB 的协议精度为毫秒，正延迟向上取整，避免截断导致提前投递。最大延迟由 nsqd 的
`max-req-timeout` 限制，超限由 broker 返回错误。小于 2 秒的发送超时会关闭协议心跳，
空闲连接可能因读超时关闭；SDK 在下次发送时重新连接。

NSQ 不提供此接口要求的 Tag/Properties、分片键、严格顺序、Tag 过滤或 ZSTD 压缩。
传入这些能力会返回可用 `errors.Is(err, mq.ErrUnsupported)` 判断的错误。
`RetryTimes > 0` 同样拒绝：适配器不自动重发结果不明的发布，以免制造隐蔽的重复消息。

## 正确性与生命周期

消费者使用 SDK 原生确认规则：handler 成功 → FIN；错误、panic、超时或关闭取消 → REQ。
关闭默认的尝试次数上限（`MaxAttempts=0`），避免超过五次尝试后 SDK 自动 FIN 丢弃消息。
重试间隔和退避复用 SDK 默认值，默认重新入队延迟 90 秒；测试 broker 为缩短实验时间将其上限设为 100ms。
消息可能重复，业务处理应幂等；本适配器没有死信队列。

发送和消费都复制 Body。handler 修改输入后失败，不会污染 SDK 保存的重试消息。

生产者按需连接，`Start()` 不执行网络就绪检查。每个实例最多有 64 个待完成发送，超过时返回
`ErrBackpressure`。取消只结束调用方等待，无法撤销已交给 NSQ 的发布；槽位会保留到 SDK 回执
或关闭，防止取消请求堆积。SDK 完成通道有一个缓冲位，迟到的回执不会阻塞 SDK 路由协程。

SDK 没有 context 版建连/入队 API，因此该阶段由 SDK I/O 超时约束，不能保证被调用方 context
立即中断。发布结果不明时，应由业务依据自身幂等策略决定是否重试。异步回调必须及时返回，
不能在回调内关闭同一个生产者。

消费者创建时安装 SDK handler，确保从未 Start、缺少业务 handler 或连接失败的实例均能关闭。
Start 失败会主动清理已建立的连接和 handler，失败实例不能重启；注册器重试时创建新实例。
重复成功 Start 和重复 Close 不重复执行操作。并发生命周期操作返回 `ErrBusy`。

Close 先禁止新的处理、取消正在执行的 handler，再停止 SDK，等待 StopChan 和实际活跃 handler
退出。handler 必须遵守 context，不能关闭自身消费者。生产者 Close 停止全部 SDK 客户端，
等待已接收的发送和回调退出；尚未确认的消息不能据此视为已成功发布。

## 验证与消融

2026-10-14，Go 1.27.2，darwin/arm64，Docker `nsqio/nsq:v1.3.0`。

单元测试覆盖校验、消息副本、PUB/MPUB/DPUB、回调、取消、背压、关闭、启动清理和无限尝试配置。
真实 nsqd 测试验证批量/异步/延迟发送、三协程消费、panic 后重新投递，最终观测到 **4 次 FIN、1 次 REQ**，
失败消息收到两次，其他消息各一次；发送响应保持空 ID，消费 ID 由 broker 提供。

[统一消融脚本](../ablation.py) 使用临时 Go overlay，每次仅退化一项机制，不修改工作区文件。完整实现必须通过；
消融版本必须触发指定测试失败；编译错误、超时均不计入有效结果。

| 消融项 | 检出的回归 |
| --- | --- |
| panic 转错误 | panic 被当成成功 |
| handler 返回后的取消检查 | 超时/关闭后的 nil 返回被当成成功 |
| 消费消息体副本 | handler 修改污染 SDK 消息 |
| 关闭时等待活跃 handler | Close 提前返回 |
| 启动失败清理 | 连接失败后未停止客户端 |
| 无限尝试配置 | 恢复 SDK 默认的五次上限 |
| 发送消息体副本 | caller 修改影响已提交 SDK 的数据 |
| 延迟向上取整 | DPUB 时间向下截断 |
| 取消后保留槽位 | 未收到回执的发送不再受容量限制 |
| 回执通道缓冲 | 迟到回执阻塞 SDK |
| 短超时心跳适配（需要真实 nsqd） | IDENTIFY 被 broker 拒绝 |

实测 **11/11 检出预期回归**。配置类实验验证 SDK 配置；真实 broker 实验验证 FIN/REQ 和协议兼容。
结果不代表已覆盖 nsqd 磁盘故障、集群失联或全部网络竞态。

从仓库根目录复现（需要 Docker）：

```sh
docker run -d --rm --name gala-nsq-test -p 127.0.0.1:14150:4150 \
  nsqio/nsq:v1.3.0 /nsqd --broadcast-address=127.0.0.1 --max-req-timeout=100ms
NSQD_TCP_ADDR=127.0.0.1:14150 go test -race -tags=integration ./components/mq/... -count=1 -timeout=90s
NSQD_TCP_ADDR=127.0.0.1:14150 python3 components/mq/ablation.py --backend nsq
docker stop gala-nsq-test
```

不设置 `NSQD_TCP_ADDR` 时，协议测试跳过，`--backend nsq` 只运行其余 10 项消融。
省略 `--backend` 时还会运行 RocketMQ 的 6 项生命周期与处理结果消融。
