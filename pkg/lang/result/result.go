// Package result provides a generic union type for value or error.
//
// Every [R] contains a value, representing success and containing a value,
// or an error, representing failure and containing an error.
// 包 result 提供值或错误的泛型联合类型。每个 [R] 要么含值表示成功，
// 要么含错误表示失败。
//
// # Simplifying the "if v, err := ...; err != nil {...}" pattern | 简化 "if v, err := ...; err != nil {...}" 模式
//
// Use [os.Open] as example:
// 以 [os.Open] 为例：
//
// The trivial way:
// 常规写法：
//
//	fd, err := os.Open("/tmp/error.log")
//	if err != nil {
//	    // Do something.
//	}
//	return fd
//
// Use result:
// 使用 result：
//
//	// Return zero value when the file is not present. | 文件不存在时返回零值
//	return Of(os.Open("/tmp/error.log")).Value()
//
//	// Return a custom file object when the file is not present. | 文件不存在时返回自定义对象
//	return Of(os.Open("/tmp/error.log")).ValueOr(os.Stderr)
//
// # JSON
//
// [R] implements [encoding/json.Marshaler] and [encoding/json.Unmarshaler], so
// you can use it in JSON marshaling/unmarshaling.
// See [result.R.MarshalJSON] and [result.R.UnmarshalJSON].
// [R] 实现了 [encoding/json.Marshaler] 与 [encoding/json.Unmarshaler]，
// 可直接参与 JSON 序列化/反序列化，详见 [result.R.MarshalJSON] 与
// [result.R.UnmarshalJSON]。
package result

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/apus-run/gala/pkg/lang/optional/of"
	"github.com/apus-run/gala/pkg/lang/value"
)

// R represents a generic result: Every result is a value,
// representing success and containing a value ([OK]),
// or an error, representing error and containing an error ([Err]).
// R 表示泛型结果：要么是含值的成功结果（[OK]），
// 要么是含错误的失败结果（[Err]）。
type R[T any] struct {
	val T
	err error
}

// Of creates a result with type T from tuple (T, error).
// Of 用 (T, error) 二元组创建类型为 T 的结果。
//
// Of is often used to wrap result of func () (T, error)`,
// see README.md for detailed usage.
// Of 常用于包装 func () (T, error) 的返回值，详细用法见 README.md。
//
// 💡 NOTE: If the given error is not nil, the value of T MUST be zero value,
// Otherwise this will be an undefined behavior.
// 💡 注意：若给定的 error 非 nil，T 的值必须是 T 的零值，否则行为未定义。
//
// ⚠️ WARNING: Passing a nil error with type (such as (*fs.PathError)(nil)) will
// cause ❌PANIC❌!
// ⚠️ 警告：传入带类型的 nil 错误（如 (*fs.PathError)(nil)）会导致 ❌PANIC❌！
func Of[T any](v T, e error) R[T] {
	checkErr(e)
	return R[T]{v, e}
}

// OK creates a result containing value v.
// OK 创建含值 v 的结果。
func OK[T any](v T) R[T] {
	return R[T]{val: v}
}

// Err creates a result containing error e.
// Err 创建含错误 e 的结果。
//
// ⚠️ WARNING: Passing a nil error will cause ❌PANIC❌!
// ⚠️ 警告：传入 nil 错误会导致 ❌PANIC❌！
func Err[T any](e error) R[T] {
	if value.IsNil(e) {
		panic(fmt.Errorf("expected a non-nil error, but found nil error with type %T", e))
	}
	return R[T]{err: e}
}

// Value returns the internal value of R.
// Value 返回 R 的内部值。
func (r R[T]) Value() T {
	return r.val
}

// ValueOr returns the internal value of R.
// Custom value v is returned when the result contains error.
// ValueOr 返回 R 的内部值；结果含错误时返回自定义值 v。
func (r R[T]) ValueOr(v T) T {
	if r.err == nil {
		return r.val
	}
	return v
}

// ValueOrZero returns the internal value of R.
// Custom value v is returned when the result contains error.
// ValueOrZero 返回 R 的内部值；结果含错误时返回 T 的零值。
//
// 💡 HINT: Refer to function [github.com/apus-run/gala/pkg/lang/value.Zero]
// for explanation of zero value.
// 💡 提示：零值的说明见 [github.com/apus-run/gala/pkg/lang/value.Zero]。
func (r R[T]) ValueOrZero() T {
	if r.err == nil {
		return r.val
	}
	return value.Zero[T]()
}

// Err returns the internal error of R.
// Err 返回 R 的内部错误。
func (r R[T]) Err() error {
	return r.err
}

// Get returns the result in (value, error) form.
// Get 以 (value, error) 形式返回结果。
func (r R[T]) Get() (T, error) {
	return r.val, r.err
}

// IsOK returns true when R contains value, otherwise false.
// IsOK 在 R 含值时返回 true，否则返回 false。
func (r R[T]) IsOK() bool {
	return r.err == nil
}

// IsErr true when R contains error, otherwise false.
// IsErr 在 R 含错误时返回 true，否则返回 false。
func (r R[T]) IsErr() bool {
	return r.err != nil
}

// IfOK calls functions f when R contains value.
// IfOK 在 R 含值时调用函数 f。
func (r R[T]) IfOK(f func(T)) {
	if r.err == nil {
		f(r.val)
	}
}

// IfErr calls functions f when R contains error.
// IfErr 在 R 含错误时调用函数 f。
func (r R[T]) IfErr(f func(error)) {
	if r.err != nil {
		f(r.err)
	}
}

func checkErr(e error) {
	if e != nil && value.IsNil(e) {
		panic(fmt.Errorf("error with type %T is nil", e))
	}
}

// typ returns the string representation of type of optional value.
// typ 返回结果类型的字符串表示。
func (r R[T]) typ() string {
	typ := reflect.TypeOf(value.Zero[T]())
	if typ == nil {
		return "any"
	}
	return typ.String()
}

// String implements fmt.Stringer.
// String 实现 fmt.Stringer 接口。
func (r R[T]) String() string {
	if r.err != nil {
		return fmt.Sprintf("gresult.Err[%s](%s)", r.typ(), r.err)
	}
	return fmt.Sprintf("gresult.OK[%s](%v)", r.typ(), r.val)
}

type jsonR[T any] struct {
	Val *T      `json:"value,omitempty"`
	Err *string `json:"error,omitempty"`
}

// MarshalJSON implements [encoding/json.Marshaler].
// MarshalJSON 实现 [encoding/json.Marshaler] 接口。
//
// Experimental: This API is experimental and may change in the future.
// 实验性：此 API 处于实验阶段，未来可能变化。
func (r R[T]) MarshalJSON() ([]byte, error) {
	jr := jsonR[T]{}
	if r.err != nil {
		jr.Err = new(r.err.Error())
	} else {
		jr.Val = &r.val
	}
	return json.Marshal(jr)
}

// UnmarshalJSON implements [encoding/json.Unmarshaler].
// UnmarshalJSON 实现 [encoding/json.Unmarshaler] 接口。
//
// ⚠️ WARNING: After unmarshaling, user MUST NOT make any assumptions about type
// type of [R.Err].
// ⚠️ 警告：反序列化之后，不要对 [R.Err] 的具体类型做任何假设。
//
// Experimental: This API is experimental and may change in the future.
// 实验性：此 API 处于实验阶段，未来可能变化。
func (r *R[T]) UnmarshalJSON(data []byte) error {
	// Unmarshalers implement UnmarshalJSON([]byte("null")) as a no-op.
	// 按惯例，Unmarshaler 对 []byte("null") 不做任何操作。
	if string(data) == "null" {
		return nil
	}
	jr := jsonR[T]{}
	if err := json.Unmarshal(data, &jr); err != nil {
		return err
	}

	// Deal with illegal JSON payload.
	// 处理非法 JSON 载荷：值与错误同时存在。
	if jr.Err != nil && jr.Val != nil {
		return errors.New("result: neither error nor value is nil")
	}

	if jr.Err == nil && jr.Val == nil {
		r.val = value.Zero[T]()
		r.err = nil
	} else if jr.Err != nil {
		r.val = value.Zero[T]()
		r.err = errors.New(*jr.Err)
	} else {
		r.val = *jr.Val
		r.err = nil
	}

	return nil
}

// Map applies function f to value of result R[F] if it contains value.
// Otherwise, passes the error of result R[F] to R[T].
// Map 在 R[F] 含值时对其应用函数 f，否则把 R[F] 的错误传递给 R[T]。
func Map[F, T any](r R[F], f func(F) T) R[T] {
	if r.err != nil {
		return Err[T](r.err)
	}
	return OK(f(r.val))
}

// MapErr applies function f to error of result R[F] if it contains error.
// Otherwise, passes the value of result R[F] to R[T].
// MapErr 在 R 含错误时对其应用函数 f，否则原样返回该结果。
func MapErr[T any](r R[T], f func(error) error) R[T] {
	if r.err == nil {
		return r
	}
	return Err[T](f(r.err))
}

// Then calls function f and returns its result if R[F] contains value.
// Otherwise, passes the error of result R[F] to R[T].
// Then 在 R[F] 含值时调用函数 f 并返回其结果，否则把错误传递给 R[T]。
//
// 💡 HINT: This function is similar to the Rust's std::result::Result.and_then
// 💡 提示：类似 Rust 的 std::result::Result.and_then。
func Then[F, T any](r R[F], f func(F) R[T]) R[T] {
	if r.err != nil {
		return Err[T](r.err)
	}
	return f(r.val)
}

// Option converts result to an optional value (a.k.a. [of.O]).
// So user can user methods provided by [of] package.
// Option 把结果转换为可选值（即 [of.O]），从而可以使用 [of] 包提供的方法。
//
//   - If the R[T] is a value, of.OK(v) is returned.
//   - If the R[T] is an error, it will be dropped, and of.Nil[T]() is returned.
//   - R[T] 为值时返回 of.OK(v)；为错误时丢弃错误并返回 of.Nil[T]()。
//
// 🚀 EXAMPLE:
//
//	OK(1).Option()         ⏩ of.OK(1)
//	err := errors.New("woof!")
//	Err[int](err).Option() ⏩ option.Nil[int]()
func (r R[T]) Option() of.O[T] {
	if r.err != nil {
		return of.Nil[T]()
	}
	return of.OK(r.val)
}
