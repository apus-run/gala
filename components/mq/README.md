# MQ

统一生产者、消费者和消费者注册器；目前支持 RocketMQ、Kafka 和 NSQ。Kafka 使用
`github.com/twmb/franz-go v1.22.2`，NSQ 使用官方 `github.com/nsqio/go-nsq v1.1.0`。

## 设计约束

公共包定义接口、配置和消息模型；后端适配器负责协议转换、确认与资源释放；注册器负责批量启动、
失败回滚与实例所有权。依赖指向公共接口，各后端独立实现自身语义。

遵循 KISS：优先直接调用、零值和小函数；新增抽象须有现有调用需求支撑。
保留核心可靠性机制时，须提供回归测试和消融证据。

## 生命周期

注册器负责自己创建的全部消费者：

```go
r := registry.NewConsumerRegistry(kafka.NewFactory())
r.Register(workers)
if err := r.StartAll(ctx); err != nil {
    return err
}
// 服务关闭时调用，并处理关闭错误。
return r.Close()
```

- 在 `StartAll` 前注册 worker；启动时读取注册列表的快照。
- 启动失败或启动期间取消，会逆序关闭本轮已创建的消费者，包括 `Start` 失败的实例。
  返回错误通过 `errors.Join` 保留启动错误和全部关闭错误，可用 `errors.Is` 判断。
- 启动失败后可重试；成功后的重复启动不重复创建消费者。
- `StartAll` 的 context 仅控制启动阶段，不能中断没有 context 参数的后端 `Start` 调用。
  运行期由显式 `Close` 管理，不额外创建取消监听协程。
- `Close` 逆序释放全部实例、聚合错误，并缓存结果；重复调用不会重复关闭。
  关闭后不能重新启动，需创建新注册器。
- 并发或重入的 `StartAll` / `Close` 返回 `registry.ErrBusy`，调用者应在当前操作结束后重试。
- 注册器把 handler panic 转为错误，防止误确认。RocketMQ、Kafka 和 NSQ 适配器直接使用时也有此保护。

兼容性变化：`mq.ConsumerRegistry` 新增 `Close() error`，自定义实现需要补充该方法；仓库内 mock 已重新生成。
NSQ 包名从误写的 `kafka` 修正为 `nsq`。

## Kafka

生产者支持同步、批量、异步发送，复用 franz-go 默认幂等生产能力。配置映射：
`Addr` → brokers；`RetryTimes` → record retries（0 表示不重试）；`ProduceTimeout` → SDK
record delivery timeout；`CompressionZSTD` → ZSTD；`ProducerGroup` → client ID，Kafka 没有对应的生产者组概念。
SDK 的 delivery timeout 不是严格的墙钟截止时间，调用方仍应传入有截止时间的 context。

`Start` 为生命周期兼容入口；客户端按需连接，不能用 `Start` 成功判断 broker 已就绪。
`SendAsync` 的参数错误直接返回；交给 SDK 后的结果通过 callback 返回，包括缓冲区已满。
callback 必须快速返回，不能在其中调用同一生产者的 `Close` 或阻塞发送。
`Close` 先等待最多 10 秒刷新，再关闭客户端，刷新错误会返回。

消息映射：

| MQ 字段 | Kafka 字段 |
| --- | --- |
| Topic | Topic |
| Body | Value 的独立副本 |
| PartitionKey | Key，相同键在分区集合不变时路由到相同分区 |
| Properties | Headers；重复 Kafka header 在接收时取最后一个值 |
| Tag | 保留 header `gala.mq.tag`，Properties 不能占用该名称 |
| MessageID / MsgID | `topic:partition:offset` |

发送开始后不要并发修改传入消息；转换完成后与 SDK 持有的数据互不影响。
`SendBatch` 会先校验全部消息；Kafka 批量发送不是事务，错误时可能已有部分消息成功。
成功响应标识最后一条输入消息，跨分区的 Offset 不可直接比较。

消费者默认使用一个处理协程，每次最多取一条消息，关闭自动提交，处理成功后同步提交。
处理或提交失败时，在阻止 rebalance 的区间内回退本地位点，随后允许 rebalance，等待 100ms 后重新拉取。
这样不会通过后续成功消息的提交跳过失败记录；仍是**至少一次**语义，业务处理必须幂等。
消息持续失败会持续重试，没有内置死信队列；可由业务处理器持久化失败记录并返回成功。

`ConsumeTimeout` 默认 30 秒，覆盖处理与提交，handler 必须遵守 context；不要配置超过组 rebalance
时限的处理预算。`Close` 取消处理并等待退出，不能在 handler 内关闭自身消费者。
无法恢复的拉取错误会记录日志、停止消费者并释放客户端，通过具体类型 `(*kafka.Consumer).Err()`
和 `Close()` 返回。关闭或终止后应创建新实例。

为保持提交顺序清晰，当前只支持 `ConsumeGoroutineNums` 为 0 或 1；扩展吞吐请使用同组多个消费者。
`Orderly` 为 true/false 都串行处理；相同分区的处理有序，但不承诺跨分区全局顺序。
Tag 表达式过滤、延迟投递、单实例多个处理协程均明确返回 `mq.ErrUnsupported`。

## NSQ

支持原生 PUB、MPUB、DPUB 和异步发送；消费失败、panic 或取消会重新入队，成功才确认。
使用 nsqd TCP 地址，ConsumerGroup 映射为 channel，支持配置处理并发数。
不支持的元数据、顺序或压缩能力明确返回错误。生命周期、取消边界、配置和真实 broker
消融结果见 [NSQ 说明](nsq/README.md)。

## RocketMQ

消息转换先复制 Body/Properties，再应用非空 Tag、PartitionKey 和正延迟，显式字段优先。
工厂启用 SDK 哈希队列选择器；普通消息仍由 SDK 选择队列。
延迟按 SDK 默认 18 档向上取整，保留超过 2 小时时截断为最大档的既有行为；不是精确定时调度，
自定义 broker 延迟档位不在此适配器支持范围内。

所有发送路径共享结果校验：无结果或非 `SendOK` 状态返回错误，同时保留可用 MessageID/Offset，
便于核对可能已写入的消息。多消息批次要求同一 Topic，并拒绝会被 SDK 批次编码丢失的延迟/分片语义；
此类消息应逐条发送。消费批次中任意消息失败，返回重试，已处理的前缀可能重复投递。

消费者只对成功启动的 SDK 实例调用 Shutdown。锁定的 SDK v2.1.2 在初始化前 Shutdown 会 panic；
初始化后的路由检查失败则由 SDK 自行清理。订阅或启动失败的适配器实例不可重启，注册器重试时创建新实例。
尚未 Start 的实例可安全 Close；成功后的重复 Start、完成后的重复 Close 均幂等，Close 缓存关闭错误。
并发生命周期操作返回 `rmq.ErrBusy`，关闭后 Start 返回 `rmq.ErrClosed`。

Close 禁止新的处理，取消并等待活跃 handler，再关闭 SDK。handler 必须遵守 context，不能关闭自身消费者。
处理上下文保留 SDK 的值和截止时间，并增加关闭取消；取消后即使 handler 返回 nil，仍返回重试。

## 验证

`components/mq` 是独立 Go module。单元测试、Kafka 内存协议测试、生命周期测试和消融从该目录运行：

```sh
cd components/mq
go test -race -tags=integration ./... -count=1 -timeout=90s
go vet ./...
python3 ablation.py
```

Kafka 协议测试使用 franz-go `kfake` 内存集群，验证 ZSTD、批量/异步发送、元数据、三分区同键路由、
handler panic 后重试和位点提交。它不依赖 Docker。NSQ 的测试细节和消融复现方法见 [NSQ 说明](nsq/README.md)。

### 真实 broker 集成测试

`integration,realbroker` 会连接一次性真实 broker，覆盖如下流程：

| 后端 | 真实服务测试覆盖 |
| --- | --- |
| Kafka | 三分区 topic、同步/批量/异步发送、ZSTD、key/tag/property 元数据、handler panic 后重投 |
| NSQ | PUB/MPUB/DPUB、异步发送、并发消费、消息 ID、失败重入队及成功确认 |
| RocketMQ | 同步/批量/异步发送、Tag/分片键/属性、延迟投递、消费失败后的 broker 重投 |

以下示例从仓库根目录运行，使用隔离 Docker 网络和 Go 测试容器。先启动三个 broker：

```sh
docker network create --subnet 172.31.240.0/24 gala-mq-real

docker run -d --rm --name gala-mq-nsq --network gala-mq-real --network-alias nsqd \
  nsqio/nsq:v1.3.0 /nsqd --broadcast-address=nsqd --max-req-timeout=100ms

docker run -d --rm --name gala-mq-kafka --network gala-mq-real --network-alias kafka \
  -e KAFKA_NODE_ID=1 \
  -e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT \
  -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT_HOST://localhost:9092,PLAINTEXT://kafka:19092 \
  -e KAFKA_PROCESS_ROLES=broker,controller \
  -e KAFKA_CONTROLLER_QUORUM_VOTERS=1@kafka:29093 \
  -e KAFKA_LISTENERS=CONTROLLER://:29093,PLAINTEXT_HOST://:9092,PLAINTEXT://:19092 \
  -e KAFKA_INTER_BROKER_LISTENER_NAME=PLAINTEXT \
  -e KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER \
  -e CLUSTER_ID=4L6g3nShT-eMCtK--X86sw \
  -e KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
  -e KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS=0 \
  -e KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1 \
  -e KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1 \
  apache/kafka:4.1.2

docker run -d --rm --name gala-mq-namesrv --network gala-mq-real --network-alias namesrv \
  apache/rocketmq:5.3.2 sh mqnamesrv

cat > /tmp/gala-mq-broker.conf <<'EOF'
brokerClusterName=DefaultCluster
brokerName=broker-a
brokerId=0
brokerIP1=172.31.240.11
autoCreateTopicEnable=true
autoCreateSubscriptionGroup=true
listenPort=10911
brokerRole=ASYNC_MASTER
flushDiskType=ASYNC_FLUSH
EOF

docker run -d --rm --name gala-mq-broker --network gala-mq-real --network-alias rmqbroker \
  --ip 172.31.240.11 \
  -v /tmp/gala-mq-broker.conf:/home/rocketmq/rocketmq-5.3.2/conf/broker.conf \
  -e NAMESRV_ADDR=namesrv:9876 \
  apache/rocketmq:5.3.2 sh mqbroker -n namesrv:9876 \
  -c /home/rocketmq/rocketmq-5.3.2/conf/broker.conf
```

等待三个服务 ready 后，运行真实测试和 race 测试：

```sh
until docker logs gala-mq-nsq 2>&1 | grep -q 'TCP: listening'; do sleep 1; done
until docker logs gala-mq-kafka 2>&1 | grep -q 'Kafka Server started'; do sleep 2; done
until docker logs gala-mq-broker 2>&1 | grep -q 'boot success'; do sleep 2; done

docker run --rm --network gala-mq-real -v "$PWD:/workspace" \
  -w /workspace/components/mq \
  -e NSQD_TCP_ADDR=nsqd:4150 \
  -e KAFKA_BROKERS=kafka:19092 \
  -e ROCKETMQ_NAMESRV_ADDR=namesrv:9876 \
  -e ROCKETMQ_BROKER_ADDR=172.31.240.11:10911 \
  golang:1.27.2 sh -c \
  'go test -tags=integration,realbroker ./... -count=1 -timeout=120s && \
   go test -race -tags=integration ./... -count=1 -timeout=90s'

docker rm -f gala-mq-broker gala-mq-namesrv gala-mq-kafka gala-mq-nsq
docker network rm gala-mq-real
rm -f /tmp/gala-mq-broker.conf
```

race 测试仍连接真实 Kafka 和 NSQ，也会运行 Kafka 的 `kfake` 测试。RocketMQ 真实 broker 用例由
`realbroker` tag 单独启用，并在不带 `-race` 的第一条命令中执行。锁定的
`rocketmq-client-go/v2 v2.1.2` 在 broker 心跳清理与消费拉取间对 SDK 内部 `processQueue` treemap 有数据竞争，
race detector 栈位于 `cleanExpiredMsg` / `putMessage`，因此该版本不能视为 race-clean。上述测试是单节点投递验证，
不覆盖生产集群的高可用、磁盘故障或网络分区。

消融入口为 [ablation.py](ablation.py)，使用临时 Go overlay，不改动工作区。每个后端先运行完整基线，
再逐项退化机制；指定测试失败且出现预期断言才算检出。编译失败、跳过、超时和死锁不算有效结果。
2026-10-14 实测 **17/17 检出预期回归**：NSQ 11 项、RocketMQ 6 项。
未设置 `NSQD_TCP_ADDR` 时明确跳过 NSQ 心跳协议实验，运行其余 16 项；可用 `--backend nsq` 或 `--backend rmq` 单独运行。

| RocketMQ 消融项 | 检出的回归 |
| --- | --- |
| 仅在成功启动后 Shutdown | 启动失败实例被错误清理 |
| 等待活跃 handler | Close 在业务处理退出前返回 |
| 关闭取消 | 活跃 handler 未收到取消 |
| 关闭门禁 | 关闭后的投递未被拒绝为 ErrClosed |
| handler 返回后的取消检查 | 处理期间取消后仍返回成功 |
| panic 转错误 | panic 未转换为处理错误 |

RocketMQ 的初始化前关闭和注册器回滚测试使用真实 SDK，无需 broker；并发关闭使用可控替身验证，
尚未覆盖真实 RocketMQ 集群的停机或网络故障。
