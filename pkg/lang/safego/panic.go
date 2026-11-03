package safego

import (
	"fmt"
)

type panicErr struct {
	info  any
	stack []byte
}

// Error implements the error interface, printing the panic info and stack | 实现 error 接口，输出 panic 信息与堆栈.
func (p *panicErr) Error() string {
	return fmt.Sprintf("panic error: %v, \nstack: %s", p.info, string(p.stack))
}

// NewPanicErr creates a new panic error.
// panicErr is a wrapper of panic info and stack trace.
// it implements the error interface, can print error message of info and stack trace.
// NewPanicErr 创建新的 panic 错误。panicErr 包装 panic 信息与栈追踪，
// 实现 error 接口，可同时打印 panic 信息与堆栈。
func NewPanicErr(info any, stack []byte) error {
	return &panicErr{
		info:  info,
		stack: stack,
	}
}
