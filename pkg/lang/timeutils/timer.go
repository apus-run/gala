package timeutils

import "time"

// TimerC obtains the time channel from a timer. This function is `nil`-safe: in case of a `nil` timer, a `nil` channel
// is returned (which is legal to read from, but reading will block forever).
// TimerC 从 timer 取出时间 channel。该函数对 `nil` 安全：timer 为 `nil` 时返回
// `nil` channel（读取合法，但会永远阻塞）。
func TimerC(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// StopTimer stops the given timer, taking care of any events that are still emitted due to a race condition.
// Note: This function must not be used if another active goroutine could read from the timer channel; otherwise,
// this function might deadlock.
// StopTimer 停止给定 timer，并处理竞态条件下仍会发出的事件。
// 注意：若可能有其他活跃 goroutine 从该 timer channel 读取，禁止使用本函数，否则可能死锁。
func StopTimer(t *time.Timer) {
	if t != nil {
		if !t.Stop() {
			<-t.C
		}
	}
}
