package slog

import (
	"fmt"
)

// sprint is a function that takes variadic any values and returns a string.
// The function works as follows:
// - If no arguments are provided, it returns an empty string.
// - If a single argument is provided:
//   - If the argument is of type string, it returns the string as is.
//   - If the argument is not of type string but implements the fmt.Stringer interface, it returns the string representation of the argument.
//   - If the argument is not of type string and does not implement the fmt.Stringer interface, it converts the argument to a string using fmt.Sprint and returns the result.
//
// - If more than one argument is provided, it converts all arguments to a string using fmt.Sprint and returns the result.
//
// sprint 接收任意个参数并返回字符串，规则如下：
// - 未提供参数时返回空字符串。
// - 只提供一个参数时：
//   - 参数是 string 类型则原样返回；
//   - 参数不是 string 但实现了 fmt.Stringer 接口，则返回其字符串表示；
//   - 两者都不满足时用 fmt.Sprint 转换后返回。
//
// - 提供多个参数时，用 fmt.Sprint 把所有参数转换为字符串后返回。
func sprint(a ...any) string {
	if len(a) == 0 {
		return ""
	} else if len(a) == 1 {
		if s, ok := a[0].(string); ok {
			return s
		} else if v, ok := a[0].(fmt.Stringer); ok {
			return v.String()
		} else {
			return fmt.Sprint(a...)
		}
	} else {
		return fmt.Sprint(a...)
	}
}

// sprintf is a function that takes a string template plus variadic any values and returns a string.
// The function works as follows:
// - If no arguments are provided, it returns the template string as is.
// - If the template string is not empty, it formats the string using fmt.Sprintf with the provided arguments and returns the result.
// - If only one argument is provided and it is of type string, it returns the string as is.
// - Otherwise, it converts the arguments to a string using the sprint function and returns the result.
//
// sprintf 接收格式模板和任意个参数并返回字符串，规则如下：
// - 未提供参数时原样返回模板字符串。
// - 模板非空时用 fmt.Sprintf 格式化后返回。
// - 只提供一个 string 类型参数时原样返回该字符串。
// - 其他情况用 sprint 函数把参数转换为字符串后返回。
func sprintf(template string, args ...any) string {
	if len(args) == 0 {
		return template
	}

	if template != "" {
		return fmt.Sprintf(template, args...)
	}

	return sprint(args...)
}
