package fileserver

import "context"

// Progress observes the transfer progress of a single call. Progress is
// injected per call and disabled by default.
//
// Callbacks of the same call run serially and progress is monotonically
// non-decreasing; no more notifications arrive after return. Callbacks do not
// run inside the Store's locks, so a callback may call other methods of the
// same Store. Callbacks must return quickly; cancellation goes through the
// Context, not Progress. Callback panics are not swallowed by the library.
// Progress only reports confirmed, deduplicated effective bytes: reaching the
// total does not mean the commit is finished. The method's return value is
// authoritative; progress must not be used to restore state.
// Progress 观察一次调用的传输进度。进度按调用注入，默认关闭。
//
// 同次调用的回调串行执行，进度单调不减；返回后不再被通知。回调不在 Store 的锁内执行，
// 因此回调中可以调用同一个 Store 的其它方法。
// 回调必须快速返回；取消走 Context，不走 Progress。回调 panic 不会被库吞掉。
// 进度只报告已确认且去重的有效字节，字节达到总量不等于提交已完成，
// 最终结果以方法返回值为准；进度不得用于恢复状态。
type Progress interface {
	Report(ctx context.Context, p ProgressInfo)
}

// ProgressInfo is a single progress notification.
// ProgressInfo 是一次进度通知。
type ProgressInfo struct {
	Key         string
	Transferred int64 // Monotonically non-decreasing; does not go backwards during retries | 单调不减；重试期间不回退
	Total       int64 // -1 when unknown, 0 for an empty object | -1 表示未知，0 表示空文件
}

// ProgressFunc lets a plain function act as Progress. A nil function's Report
// is a no-op.
// ProgressFunc 让普通函数直接充当 Progress。nil 函数的 Report 为空操作。
type ProgressFunc func(context.Context, ProgressInfo)

// Report implements Progress.
// Report 实现 Progress。
func (f ProgressFunc) Report(ctx context.Context, p ProgressInfo) {
	if f != nil {
		f(ctx, p)
	}
}
