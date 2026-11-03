// https://github.com/Southclaws/opt

// Package opt provides a container type [Optional] that wraps a value which may
// or may not be present, easing conditional access and transformation.
// 包 opt 提供包装"可能有值也可能没有"的容器类型 [Optional]，简化条件访问与变换。
package opt

import (
	"encoding/json"
	"fmt"
)

// Optional wraps the type `T` within a container which provides abstractions
// to make conditional access and transformation of optional types easier.
// Optional 将类型 T 包装进一个容器，为可选类型的条件访问与变换提供抽象。
type Optional[T any] container[T]

// -
// Constructors
// -

// NewEmpty creates an empty optional of the specified type `T`.
// NewEmpty 创建一个指定类型 T 的空 Optional。
func NewEmpty[T any]() Optional[T] { return nil }

// New wraps the input value in an optional type.
// New 将输入值包装为 Optional。
func New[T any](value T) Optional[T] { return Optional[T]{value} }

// NewMap wraps `v` after applying `fn` and producing a new type `R`.
// NewMap 先对 v 应用 fn 得到新类型 R，再包装为 Optional。
func NewMap[T, R any](v T, fn func(T) R) Optional[R] {
	return New(fn(v))
}

// NewSafe works with common "safe" APIs that return (T, boolean)
// NewSafe 适配常见的返回 (T, boolean) 的 "safe" 风格 API。
func NewSafe[T any](value T, ok bool) Optional[T] {
	if ok {
		return Optional[T]{value}
	}
	return nil
}

// NewIf wraps `v` if `fn` returns true. Useful for sanitisation of input such
// as trimming spaces and treating empty strings as none.
// NewIf 在 fn 返回 true 时包装 v。适合输入清洗，如去空格、把空串视为无值。
func NewIf[T any](v T, fn func(T) bool) Optional[T] {
	if fn(v) {
		return New(v)
	}
	return NewEmpty[T]()
}

// NewPtr wraps the input if it's non-nil, otherwise returns an empty optional.
// NewPtr 在指针非 nil 时包装其指向的值，否则返回空 Optional。
func NewPtr[T any](ptr *T) Optional[T] {
	if ptr == nil {
		return NewEmpty[T]()
	}
	return New(*ptr)
}

// NewPtrMap wraps the input if it's non-nil and applies the transformer
// function, otherwise returns an empty optional.
// NewPtrMap 在指针非 nil 时应用变换函数并包装结果，否则返回空 Optional。
func NewPtrMap[T, R any](ptr *T, fn func(T) R) Optional[R] {
	if ptr == nil {
		return NewEmpty[R]()
	}
	return New(fn(*ptr))
}

// NewPtrIf is the same as `NewIf` except will return empty if `ptr` is nil.
// NewPtrIf 与 NewIf 相同，但指针为 nil 时直接返回空 Optional。
func NewPtrIf[T any](ptr *T, fn func(T) bool) Optional[T] {
	if ptr == nil {
		return NewEmpty[T]()
	}
	if !fn(*ptr) {
		return NewEmpty[T]()
	}
	return New(*ptr)
}

// NewPtrOr wraps the input if it's non-nil, otherwise returns a fallback value.
// NewPtrOr 在指针非 nil 时包装其指向的值，否则包装 fallback。
func NewPtrOr[T any](ptr *T, fallback T) Optional[T] {
	if ptr == nil {
		return New(fallback)
	}
	return New(*ptr)
}

// -
// Accessors
// -

// Ok returns true if there's a value inside.
// Ok 在容器内有值时返回 true。
func (o Optional[T]) Ok() bool {
	return o != nil
}

// Get returns the wrapped value if it's present, `ok` signals existence.
// Get 返回包装的值，ok 表示是否有值。
func (o Optional[T]) Get() (value T, ok bool) {
	if o == nil {
		return
	}
	return o[0], true
}

// GetMap returns the wrapped value if it's present and applies `fn`.
// GetMap 在有值时对包装值应用 fn 并返回。
func GetMap[In, Out any](in Optional[In], fn func(In) Out) (v Out, ok bool) {
	if in == nil {
		return
	}
	return fn(in[0]), true
}

// GetMapC is the curried version of GetMap. See `Map` for an example.
// GetMapC 是 GetMap 的柯里化版本，示例见 Map。
func GetMapC[In, Out any](fn func(In) Out) func(in Optional[In]) (v Out, ok bool) {
	return func(in Optional[In]) (v Out, ok bool) {
		return GetMap(in, fn)
	}
}

// Ptr turns an optional value into a pointer to that value or nil.
// Ptr 将可选值转为指向该值的指针，无值时返回 nil。
func (o Optional[T]) Ptr() *T {
	if o == nil {
		return nil
	}
	return &o[0]
}

// PtrMap turns an optional value into a pointer to that value then transforms
// the value to a new type using the given transformer function.
// PtrMap 将可选值转为指针，并用给定变换函数把值转换为新类型。
func PtrMap[In any, Out any](o Optional[In], fn func(In) Out) *Out {
	if val, ok := o.Get(); ok {
		v := fn(val)
		return &v
	}
	return nil
}

// PtrMapC is the curried version of PtrMap. See `Map` for an example.
// PtrMapC 是 PtrMap 的柯里化版本，示例见 Map。
func PtrMapC[In any, Out any](fn func(In) Out) func(o Optional[In]) *Out {
	return func(in Optional[In]) *Out {
		return PtrMap(in, fn)
	}
}

// Or returns the underlying value or `v`.
// Or 返回内部值，无值时返回 v。
func (o Optional[T]) Or(v T) (value T) {
	if o == nil {
		return v
	}

	return o[0]
}

// Call calls `fn` if there is a value wrapped by this optional.
// Call 在有包装值时调用 fn。
func (o Optional[T]) Call(f func(value T)) {
	if o != nil {
		f(o[0])
	}
}

// OrCall calls `fn` if the optional is empty.
// OrCall 在 Optional 为空时调用 fn。
func (o Optional[T]) OrCall(fn func() T) (value T) {
	if o != nil {
		return o[0]
	}

	return fn()
}

// OrZero returns the zero value of `T` if it's not present.
// OrZero 无值时返回 T 的零值。
func (o Optional[T]) OrZero() (value T) {
	if o == nil {
		var zero T
		return zero
	}

	return o[0]
}

// -
// Conditional pipelines
// -

// Map calls `fn` on `in` if it's present and returns the new optional value.
// Map 在 in 有值时对其应用 fn，并返回新的 Optional。
func Map[In, Out any](in Optional[In], fn func(In) Out) (v Optional[Out]) {
	if in == nil {
		return
	}

	return Optional[Out]{fn(in[0])}
}

// MapC is the curried version of Map. It's useful for applying the same mapping
// function to many values either manually or using a functor library.
// MapC 是 Map 的柯里化版本，适合手工或用 functor 库把同一映射函数应用到多个值。
//
// First, construct the function with the transformer.
// 首先，用变换函数构造该函数。
//
//	fn := Map(transformer)
//
// Now you can use `fn` to transform any optional value using the transformer.
// 之后就可以用 fn 对任意可选值应用该变换。
//
//	b := fn(a)
//	dt.Map(array, fn)
func MapC[In, Out any](fn func(In) Out) func(in Optional[In]) (v Optional[Out]) {
	return func(in Optional[In]) (v Optional[Out]) {
		return Map(in, fn)
	}
}

// MapErr calls `fn` on `in` if it's present and returns the result or an error.
// MapErr 在 in 有值时对其应用 fn，返回结果或错误。
func MapErr[In, Out any](in Optional[In], fn func(In) (Out, error)) (v Optional[Out], err error) {
	if in == nil {
		return
	}

	out, err := fn(in[0])
	if err != nil {
		return nil, err
	}

	return Optional[Out]{out}, nil
}

// MapErrC calls `fn` on `in` if it's present and returns the result or an error.
// MapErrC 是 MapErr 的柯里化版本，行为相同。
func MapErrC[In, Out any](fn func(In) (Out, error)) func(in Optional[In]) (v Optional[Out], err error) {
	return func(in Optional[In]) (v Optional[Out], err error) {
		return MapErr(in, fn)
	}
}

// -
// Utilities
// -

// String returns the string representation of the value or an empty string.
// String 返回值的字符串表示，无值时返回空字符串。
func (o Optional[T]) String() string {
	if v, ok := o.Get(); ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// GoString is only used for verbose printing.
// GoString 仅用于详细打印（%#v）。
func (o Optional[T]) GoString() string {
	if v, ok := o.Get(); ok {
		return fmt.Sprintf("Optional[%v]", v)
	}
	return "Optional[]"
}

// MarshalJSON marshals the value being wrapped to JSON. If there is no value
// being wrapped, the zero value of its type is marshaled.
// MarshalJSON 将包装的值序列化为 JSON；没有包装值时输出 null。
func (o Optional[T]) MarshalJSON() (data []byte, err error) {
	if v, ok := o.Get(); ok {
		return json.Marshal(v)
	}
	return []byte("null"), nil
}

// UnmarshalJSON unmarshals the JSON into a value wrapped by this optional.
// UnmarshalJSON 将 JSON 反序列化为 Optional 包装的值。
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*o = NewEmpty[T]()
		return nil
	}

	var v T
	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}

	*o = New(v)
	return nil
}

// container is an internal type that hides and holds the underlying value.
// container 是隐藏并持有底层值的内部类型。
type container[T any] []T
