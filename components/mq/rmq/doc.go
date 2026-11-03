// Package rmq implements the mq interfaces on Apache RocketMQ via
// rocketmq-client-go/v2 | 基于 rocketmq-client-go/v2 在 Apache RocketMQ 上
// 实现 mq 接口.
//
// # Notes | 说明
//
// Addr entries are name-server addresses handed to a custom resolver that
// also resolves hostnames via DNS. Delayed delivery uses RocketMQ's fixed
// delay levels: a DeferDuration is rounded up to the first level that covers
// it (1s to 2h) and clamped at the largest.
// Addr 是 name server 地址，交给自定义 resolver 处理（还会对主机名做 DNS
// 解析）。延迟投递使用 RocketMQ 固定的延迟级别：DeferDuration 向上取整到
// 第一个能覆盖它的级别（1 秒到 2 小时），超出则取最大级别。
//
// Consumers are push consumers: a handler error returns ConsumeRetryLater so
// RocketMQ redelivers; Orderly enables orderly consumption and TagExpression
// sets a TAG filter expression. Handler panics are recovered. Batches must
// share one topic, and delayed or sharded batches are rejected
// (mq.ErrUnsupported) because the SDK drops those properties on the outer
// batch. Compression is not supported.
// 消费者是 push 消费者：处理器出错返回 ConsumeRetryLater，由 RocketMQ 重投；
// Orderly 开启顺序消费，TagExpression 作为 TAG 过滤表达式。处理器 panic
// 会被恢复。批量消息必须同一主题，且延迟或分片批量被拒绝
// （mq.ErrUnsupported），因为 SDK 会丢弃外层批量消息上的这些属性。
// 压缩不支持。
//
// # Features | 功能
//
// - Sync, batch and callback-based async sending | 同步、批量与回调式异步发送
// - Sharding-key ordered delivery and tag filtering | 分片键顺序投递与标签过滤
package rmq
