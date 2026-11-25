// Package bitring 提供基于位图的滑动窗口事件统计。
// 它以极低的内存开销记录最近 N 次事件结果，并按「连续发生次数」或
// 「事件发生率」判定是否满足触发条件，适合错误率熔断等场景。
//
// Package bitring keeps a sliding window of recent event outcomes in a bitmap,
// then reports whether a consecutive-run or event-rate condition is met.
package bitring
