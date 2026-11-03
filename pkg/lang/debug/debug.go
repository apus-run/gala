// Package debug provides small helpers for stack traces and memory
// diagnostics during development.
// 包 debug 提供开发期用于栈追踪与内存诊断的小工具。
package debug

import (
	"runtime/debug"
)

// FreeOSMemory runs a GC and then tries to relinquish as much memory back to
// the OS as possible.
// FreeOSMemory 先执行一次 GC，再尽可能把内存归还给操作系统。
func FreeOSMemory() {
	debug.FreeOSMemory()
}
