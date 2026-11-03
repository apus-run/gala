// Package kafka implements the mq interfaces on Kafka via franz-go
// | 基于 franz-go 在 Kafka 上实现 mq 接口.
//
// # Notes | 说明
//
// Consumers use Kafka consumer groups with autocommit disabled: records are
// processed one at a time and a record is committed only after its handler
// succeeds, so a failing record is retried until it succeeds or its retries
// are exhausted.
// 消费者使用 Kafka 消费者组并关闭自动提交：记录逐条串行处理，处理器成功后才
// 提交该记录，失败的记录会一直重试，直到成功或重试次数耗尽。
//
// Not supported by this backend: concurrent handlers (ConsumeGoroutineNums
// > 1), tag filtering (TagExpression) and delayed delivery (DeferDuration) —
// each fails with mq.ErrUnsupported. Compression supports none or ZSTD only.
// ProducerConfig.ProducerGroup only sets the client ID; Kafka producers hold
// no group state.
// 本后端不支持：并发消费（ConsumeGoroutineNums > 1）、标签过滤
// （TagExpression）与延迟投递（DeferDuration），均返回 mq.ErrUnsupported。
// 压缩仅支持不压缩与 ZSTD。ProducerConfig.ProducerGroup 仅用作 Client ID，
// Kafka 生产者没有组状态。
//
// When WithMaxRetries is set and exhausted, the record is forwarded to the
// dead-letter topic configured with WithDeadLetterTopic — its offset is
// committed only after the forward succeeds — or, with no dead letter,
// logged and skipped, so it stops blocking its partition.
// 设置 WithMaxRetries 且耗尽后，记录被转入 WithDeadLetterTopic 配置的
// 死信 topic（转发成功后才提交位移）；未配置死信则记录日志后跳过，
// 不再阻塞所在分区。
//
// # Features | 功能
//
// - Sync, batch and callback-based async producing | 同步、批量与回调式异步发送
// - Serial consume with per-record commit, bounded retry and optional dead letter | 串行消费、按记录提交、有界重试与可选死信
// - MessageID takes the form topic:partition:offset | MessageID 形如 topic:partition:offset
package kafka
