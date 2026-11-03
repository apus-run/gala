// Package mq defines the shared interfaces, configs and message model of the
// gala message-queue components | mq 定义 gala 消息队列组件的公共接口、配置与消息模型.
//
// # Notes | 说明
//
// This package contains abstractions only: Producer, Consumer, Factory, the
// Message model and ConsumerRegistry. Backend adapters live in separate
// modules (mq/kafka, mq/nsq, mq/rmq, mq/rds) and are not registered here;
// callers pick a backend by importing its module and passing its Factory to
// registry.NewConsumerRegistry. Backend capabilities are not aligned: an
// operation a backend cannot honor fails with ErrUnsupported.
// 本包只包含抽象：Producer、Consumer、Factory、Message 模型与 ConsumerRegistry。
// 后端适配器位于独立 module（mq/kafka、mq/nsq、mq/rmq、mq/rds），并不在本包注册；
// 调用方按需导入后端 module，把它的 Factory 传给 registry.NewConsumerRegistry。
// 各后端能力不对齐：后端不支持的操作以 ErrUnsupported 失败。
//
// # Features | 功能
//
// - Sync, batch and callback-based async sending | 同步、批量与带回调的异步发送
// - Message builders for tags, ordering keys, properties and defer delays | 消息构造器：标签、顺序键、属性与延迟
// - Batch consumer startup with rollback, via the registry subpackage | registry 子包提供批量启动与失败回滚
package mq
