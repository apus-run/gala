package errorsx

// Must is a helper that wraps a call to a function returning value and error
// and panics if the error is non-nil.
// Must 包装一个返回 (值, error) 的函数调用，error 非 nil 时 panic。
func Must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}

	return val
}

// MustRuntime is a runtime version of errorsx.Must().
// MustRuntime 是 errorsx.Must() 的运行时版本。
func MustRuntime(val any, err error) any {
	if err != nil {
		panic(err)
	}

	return val
}
