// Package batchsize 提供根据响应时间动态调整批处理大小的策略实现。
// 适用于批量消费、批量落库等需要按下游压力自适应调节吞吐量的场景。
//
// Package batchsize adapts batch sizes at runtime based on observed latency,
// which suits batch consumption or batch persistence under variable load.
package batchsize
