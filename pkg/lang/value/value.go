// Package value provides utility functions for working with values in Go.
// 包 value 提供处理 Go 值的实用函数。
// # Operations
//
//   - Math operations: [Max], [Min], [MinMax], [Clamp], …
//   - Type assertion (T1 → T2): [TypeAssert], [TryAssert], …
//   - Predicate: (T → bool): [Equal], [Greater], [Less], [Between], [IsNil], [IsZero], …
package value

import (
	"reflect"

	"github.com/apus-run/gala/pkg/lang/constraints"
)

// Zero returns zero value of type.
// Zero 返回类型的零值。
//
// The zero value is:
//
//   - 0 for numeric types,
//   - false for the boolean type
//   - "" (the empty string) for strings
//   - nil for reference/pointer type
//
// 零值是：
//
//   - 数值类型为 0
//   - 布尔类型为 false
//   - 字符串为 ""（空字符串）
//   - 引用/指针类型为 nil
func Zero[T any]() (v T) {
	return
}

// Or returns the first non-zero value of inputs.
// If all values are zero, return the zero value of type.
// Or 返回入参中第一个非零值；全为零值时返回类型零值。
//
// 🚀 EXAMPLE:
//
//	Or(false, true)  ⏩ true
//	Or(0, 1, 2)      ⏩ 1
//	Or("", "1", "2") ⏩ "1"
//	Or(0, 0, 0)      ⏩ 0
//	Or("", "", "")   ⏩ ""
func Or[T comparable](vals ...T) (v T) {
	for _, val := range vals {
		if val != v {
			return val
		}
	}
	return
}

// Max returns the maximum value of inputs.
// Max 返回入参中的最大值。
//
// 🚀 EXAMPLE:
//
//	Max(1, 2)            ⏩ 2
//	Max(1, 2, 3)         ⏩ 3
//	Max("2", "10", "11") ⏩ "2"
func Max[T constraints.Ordered](x T, y ...T) T {
	max := x
	for _, v := range y {
		if v > max {
			max = v
		}
	}
	return max
}

// Min returns the minimum value of inputs.
// Min 返回入参中的最小值。
//
// 🚀 EXAMPLE:
//
//	Min(1, 2)            ⏩ 1
//	Min(1, 2, 3)         ⏩ 1
//	Min("2", "10", "11") ⏩ "10"
func Min[T constraints.Ordered](x T, y ...T) T {
	min := x
	for _, v := range y {
		if v < min {
			min = v
		}
	}
	return min
}

// MinMax returns the minimum value and maximum value of inputs.
// MinMax 返回入参中的最小值和最大值。
//
// 🚀 EXAMPLE:
//
//	MinMax(1, 2)            ⏩ 1, 2
//	MinMax(1, 2, 3)         ⏩ 1, 3
//	MinMax("2", "10", "11") ⏩ "10", "2"
func MinMax[T constraints.Ordered](x T, y ...T) (T, T) {
	min, max := x, x
	for _, v := range y {
		if min > v {
			min = v
		} else if max < v {
			max = v
		}
	}
	return min, max
}

// Clamp returns the value if the value is within [min, max]; otherwise
// returns the nearest boundary. If min is greater than max, the behavior is
// undefined.
// Clamp 当 value 在 [min, max] 内时原样返回，否则返回最近的边界；
// min 大于 max 时行为未定义。
//
// 🚀 EXAMPLE:
//
//	Clamp(1, 2, 3)         ⏩ 2
//	Clamp(2, 1, 3)         ⏩ 2
//	Clamp(3, 1, 2)         ⏩ 2
//	Clamp("2", "10", "11") ⏩ "11"
func Clamp[T constraints.Ordered](value, min, max T) T {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// IsNil returns whether the given value v is nil.
// IsNil 判断给定值 v 是否为 nil。
//
// 💡 NOTE: Typed nil interface (such as fmt.Stringer((*net.IP)(nil))) is nil,
// although fmt.Stringer((*net.IP)(nil)) != nil.
// 💡 说明：带类型的 nil 接口（如 fmt.Stringer((*net.IP)(nil))）视为 nil，
// 尽管 fmt.Stringer((*net.IP)(nil)) != nil。
//
// 🚀 EXAMPLE:
//
//	IsNil(nil)                           ⏩ true
//	IsNil(1)                             ⏩ false
//	IsNil((*int)(nil))                   ⏩ true
//	IsNil(fmt.Stringer((*net.IP)(nil)))  ⏩ true
func IsNil(v any) bool {
	if v == nil {
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map,
		reflect.Pointer, reflect.UnsafePointer,
		reflect.Interface, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// IsNotNil is negation of [IsNil].
// IsNotNil 是 [IsNil] 的取反。
func IsNotNil(v any) bool {
	return !IsNil(v)
}

// IsZero returns whether the given v is zero value.
// IsZero 判断给定 v 是否为零值。
//
// 💡 HINT: Refer to function [Zero] for explanation of zero value.
// 💡 提示：零值的说明参见 [Zero]。
func IsZero[T comparable](v T) bool {
	return v == Zero[T]()
}

// IsNotZero is negation of [IsZero].
// IsNotZero 是 [IsZero] 的取反。
func IsNotZero[T comparable](v T) bool {
	return v != Zero[T]()
}

// Equal returns whether the given x and y are equal.
// Equal 判断 x 与 y 是否相等。
func Equal[T comparable](x, y T) bool {
	return x == y
}

// Add adds given values x and y and returns the sum.
// For string, Add performs concatenation.
// Add 返回 x 与 y 相加之和；对字符串执行拼接。
func Add[T constraints.Number | constraints.Complex | ~string](x, y T) T {
	return x + y
}

// TypeAssert converts a value from type From to type To by [type assertion].
// TypeAssert 通过[类型断言]把值从 From 类型转为 To 类型。
//
// ⚠️ WARNING: *Type assertion* is not type conversion/casting, it means that:
// ⚠️ 警告：*类型断言* 不是类型转换/强转，它意味着：
//
//  1. It may ❌PANIC❌ when type assertion failed
//  2. You can NOT cast int values to int8, can NOT cast int value to string
//  3. You can cast interface value to int if its internal value is an int
//
// 即：断言失败时可能 ❌PANIC❌；不能把 int 断言成 int8，也不能把 int 断言成
// string；但接口内部值是 int 时可以断言成 int。
//
// 💡 NOTE: The first type parameter is result type (To), which means you can
// omit the original type (From) via type inference.
// 💡 说明：第一个类型参数是结果类型（To），因此可以借助类型推断省略原类型（From）。
//
// [type assertion]: https://go.dev/tour/methods/15
func TypeAssert[To, From any](v From) To {
	return any(v).(To)
}

// TryAssert tries to convert a value from type From to type To by
// [type assertion].
// TryAssert 尝试通过类型断言把值从 From 类型转为 To 类型，失败时不 panic。
func TryAssert[To, From any](v From) (To, bool) {
	to, ok := any(v).(To)
	return to, ok
}

// Less returns true when x is less than y, otherwise false.
// Less 当 x 小于 y 时返回 true，否则返回 false。
func Less[T constraints.Ordered](x, y T) bool {
	return x < y
}

// LessEqual returns true when x is less than or equal to y, otherwise false.
// LessEqual 当 x 小于等于 y 时返回 true，否则返回 false。
func LessEqual[T constraints.Ordered](x, y T) bool {
	return x <= y
}

// Greater returns true when x is greater than y, otherwise false.
// Greater 当 x 大于 y 时返回 true，否则返回 false。
func Greater[T constraints.Ordered](x, y T) bool {
	return x > y
}

// GreaterEqual returns true when x is greater than or equal to y, otherwise
// false.
// GreaterEqual 当 x 大于等于 y 时返回 true，否则返回 false。
func GreaterEqual[T constraints.Ordered](x, y T) bool {
	return x >= y
}

// Between returns true when v is within [min, max], otherwise false.
// Between 当 v 落在 [min, max] 内时返回 true，否则返回 false。
func Between[T constraints.Ordered](v, min, max T) bool {
	return v >= min && v <= max
}
