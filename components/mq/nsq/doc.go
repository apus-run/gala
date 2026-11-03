// Package nsq implements the mq interfaces on NSQ via nsqio/go-nsq
// | 基于 nsqio/go-nsq 在 NSQ 上实现 mq 接口.
//
// # Notes | 说明
//
// Addr entries must be nsqd TCP addresses: producers publish directly to the
// nsqd list in Addr (round-robin per publish), and consumers connect to the
// same list; no nsqlookupd discovery is performed.
// Addr 必须是 nsqd 的 TCP 地址：生产者直接向 Addr 中的 nsqd 列表发布
// （每次发布轮询选择），消费者连接同一批 nsqd；不经过 nsqlookupd 服务发现。
//
// Consumers map ConsumerGroup to an NSQ channel, disable the SDK's default
// attempt cap (MaxAttempts=0) so a failing message is requeued instead of
// being silently FINished, and run up to ConsumeGoroutineNums handlers in
// flight. Orderly consumption and tag filtering are unsupported
// (mq.ErrUnsupported).
// 消费者把 ConsumerGroup 映射为 NSQ channel，关闭 SDK 默认的尝试上限
// （MaxAttempts=0），失败消息被重新入队而不是被静默 FIN；
// 最多 ConsumeGoroutineNums 个处理器并发在途。顺序消费与标签过滤不支持
// （返回 mq.ErrUnsupported）。
//
// Producers publish asynchronously under the hood with at most 64 pending
// sends (ErrBackpressure beyond that); publish retries and compression are
// unsupported. Tags, properties and partition keys are rejected; delayed
// delivery is supported for single messages only (DPUB, rounded up to whole
// milliseconds) and batches must share one topic. NSQ returns no broker
// message id, so SendResponse.MessageID stays empty.
// 生产者底层异步发布，未完成发送最多 64 个（超出返回 ErrBackpressure）；
// 发布重试与压缩不支持。Tag、属性与分区键被拒绝；延迟投递仅支持单条消息
// （DPUB，向上取整到毫秒），批量消息必须同一主题。NSQ 不返回 broker 消息
// id，SendResponse.MessageID 恒为空。
//
// # Features | 功能
//
// - Sync, batch and callback-based async sending | 同步、批量与回调式异步发送
// - Deferred publish for single messages | 单条消息的延迟发布
package nsq
