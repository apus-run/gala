package errorsx

// Ignore is a helper that wraps a call to a function returning value and error
// and ignores if the error is non-nil.
// Ignore 包装一个返回 (值, error) 的函数调用，error 非 nil 时直接忽略。
func Ignore[T any](val T, err error) T {
	return val
}

// IgnoreRuntime is a runtime version of errorsx.Ignore().
// IgnoreRuntime 是 errorsx.Ignore() 的运行时版本。
func IgnoreRuntime(val any, err error) any {
	return val
}
