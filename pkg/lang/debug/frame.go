package debug

import (
	"fmt"
	"runtime"
	"strings"
)

// FrameToString converts a stack frame to a string, in the format
// `<function> (<file>:<line>)`.
// FrameToString 将栈帧转为字符串，格式为 `<function> (<file>:<line>)`。
func FrameToString(frame runtime.Frame) string {
	funcName := frame.Function
	if funcName == "" {
		funcName = "<unknown>"
	}
	return fmt.Sprintf("%s (%s:%d)", funcName, frame.File, frame.Line)
}

// FramesToString converts a `Frames` object to a newline-separated list of
// frame strings.
// FramesToString 将 `Frames` 对象转换为以换行分隔的栈帧字符串列表。
func FramesToString(frames *runtime.Frames) string {
	var b strings.Builder
	for {
		frame, more := frames.Next()

		_, _ = b.WriteString(FrameToString(frame))
		_, _ = b.WriteString("\n")

		if !more {
			break
		}
	}
	return b.String()
}
