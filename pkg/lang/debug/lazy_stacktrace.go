package debug

import "runtime"

// LazyStacktrace is a stack trace that is cheap to construct, and will
// resolve frames to source/function information on demand.
// LazyStacktrace 是构造开销很小的栈追踪，按需再把栈帧解析为源码/函数信息。
type LazyStacktrace []uintptr

const (
	maxStacktrace = 10
)

// GetLazyStacktrace returns a lazy stacktrace, skipping the given number of
// frames. A skip value of 0 indicates that the frame for GetLazyStacktrace
// should be part of the stack trace.
// GetLazyStacktrace 返回一个惰性栈追踪，并跳过指定数量的栈帧；
// skip 为 0 表示 GetLazyStacktrace 自身所在的栈帧也计入栈追踪。
func GetLazyStacktrace(skip int) LazyStacktrace {
	callerPCs := make([]uintptr, maxStacktrace)
	numCallers := runtime.Callers(skip+1, callerPCs)
	return LazyStacktrace(callerPCs[:numCallers])
}

// String returns a string representation of the stacktrace.
// String 返回栈追踪的字符串表示。
func (t LazyStacktrace) String() string {
	frames := runtime.CallersFrames([]uintptr(t))
	return FramesToString(frames)
}
