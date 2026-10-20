package fileserver

import "context"

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

// ProgressInfo 是一次进度通知。
type ProgressInfo struct {
	Key         string
	Transferred int64 // 单调不减；重试期间不回退
	Total       int64 // -1 表示未知，0 表示空文件
}

// ProgressFunc 让普通函数直接充当 Progress。nil 函数的 Report 为空操作。
type ProgressFunc func(context.Context, ProgressInfo)

// Report 实现 Progress。
func (f ProgressFunc) Report(ctx context.Context, p ProgressInfo) {
	if f != nil {
		f(ctx, p)
	}
}
