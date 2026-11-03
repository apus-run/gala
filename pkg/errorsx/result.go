package errorsx

// Result is a generic type that wraps a value and an error.
// It also provides a set of methods to resolve/unwrap underlying value
// with a specific value/error handling.
// Result 是包装值与错误的泛型类型，
// 并提供一组按指定方式处理值/错误来解析或解包底层值的方法。
type Result[T any] struct {
	value T
	error error
}

// Simple value getters | 简单取值方法

// Error returns the wrapped error | Error 返回包装的错误.
func (r Result[T]) Error() error {
	return r.error
}

// Value returns the wrapped value | Value 返回包装的值.
func (r Result[T]) Value() T {
	return r.value
}

// Advanced value getters | 进阶取值方法

// ValueOr returns the value if no error is present,
// otherwise returns the provided default value
// ValueOr 无错误时返回值，否则返回提供的默认值。
func (r Result[T]) ValueOr(def T) T {
	if r.error != nil {
		return def
	}

	return r.value
}

// Must returns the value if no error is present,
// otherwise panics with the error
// Must 无错误时返回值，否则以错误 panic。
func (r Result[T]) Must() T {
	return Must(r.value, r.error)
}

// Case handlers | 分支处理方法

// Then executes the provided function if no error is present
// Then 无错误时执行给定函数.
func (r Result[T]) Then(fn func(T)) Result[T] {
	if r.error == nil {
		fn(r.value)
	}

	return r
}

// Catch executes the provided function if an error is present
// Catch 有错误时执行给定函数.
func (r Result[T]) Catch(fn func(error)) Result[T] {
	if r.error != nil {
		fn(r.error)
	}

	return r
}

// Wrap wraps a value and an error into a Result | Wrap 把值与错误包装成 Result.
func Wrap[T any](val T, err error) Result[T] {
	return Result[T]{value: val, error: err}
}
