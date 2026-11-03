// Package ptr provides some functions for pointer-based operations.
// 包 ptr 提供若干基于指针的操作函数。
package ptr

import (
	"fmt"
	"reflect"
)

// Of returns a pointer that points to equivalent value of value v.
// (T → *T).
// It is useful when you want to "convert" a unaddressable value to pointer.
// Of 返回指向与 v 等值的指针（T → *T），适合把不可寻址的值“变成”指针。
//
// If you need to assign the address of a literal to a pointer:
// 需要把字面量的地址赋给指针时：
//
//	 payload := struct {
//		    Name *string
//	 }
//
// The practice without generic:
// 不用泛型的写法：
//
//	x := "name"
//	payload.Name = &x
//
// Use generic:
// 使用泛型：
//
//	payload.Name = Of("name")
//
// 💡 HINT: use [Indirect] to dereference pointer (*T → T).
// 💡 提示：解引用指针（*T → T）请用 [Indirect]。
//
// ⚠️  WARNING: The returned pointer does not point to the original value because
// Go is always pass by value, user CAN NOT modify the value by modifying the pointer.
// ⚠️ 警告：Go 总是按值传递，返回的指针并不指向原值，
// 用户无法通过该指针修改原值。
func Of[T any](v T) *T {
	return new(v)
}

// ToPtr returns a pointer to the given value.
// ToPtr 返回指向给定值的指针。
func ToPtr[T any](v T) *T {
	return new(v)
}

// From returns the value pointed to by the pointer p.
// If the pointer is nil, returns the zero value of T instead.
// From 返回指针指向的值；指针为 nil 时返回 T 的零值。
func From[T any](v *T) T {
	var zero T
	if v != nil {
		return *v
	}

	return zero
}

// FromOrDefault dereferences ptr and returns the value it points to if no nil, or else
// returns def.
// FromOrDefault 解引用指针并返回其指向的值；指针为 nil 时返回 def。
func FromOrDefault[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}

// Integer is a constraint of all built-in signed integer types.
// Integer 约束所有内建有符号整数类型。
type Integer interface {
	~int64 | ~int32 | ~int16 | ~int8 | ~int
}

// ConvIntPtr converts a pointer of integer type T to a pointer of integer
// type K; nil stays nil.
// ConvIntPtr 把整数类型 T 的指针转换为整数类型 K 的指针；nil 保持 nil。
func ConvIntPtr[T, K Integer](p *T) *K {
	if p == nil {
		return nil
	}
	return new((K)(*p))
}

// Indirect returns the value pointed to by the pointer p.
// If the pointer is nil, returns the zero value of T instead.
// Indirect 返回指针指向的值；指针为 nil 时返回 T 的零值。
//
// 🚀 EXAMPLE:
//
//	v := 1
//	var ptrV *int = &v
//	var ptrNil *int
//	Indirect(ptrV)    ⏩ 1
//	Indirect(ptrNil)  ⏩ 0
//
// 💡 HINT: Refer [github.com/apus-run/gala/pkg/lang/value.Zero] for definition of zero value.
// 💡 提示：零值定义见 [github.com/apus-run/gala/pkg/lang/value.Zero]。
//
// 💡 AKA: Unref, Unreference, Deref, Dereference
func Indirect[T any](p *T) (v T) {
	if p == nil {
		// Explicitly return value.Zero causes an extra copy.
		// return value.Zero[T]()
		// 显式返回 value.Zero 会多一次拷贝，故直接返回零值。
		return // the initial value is zero value, see also [Indirect_valueZero].
	}
	return *p
}

// IndirectOr is a variant of [Indirect],
// If the pointer is nil, returns the fallback value instead.
// IndirectOr 是 [Indirect] 的变体：指针为 nil 时返回 fallback 值。
//
// 🚀 EXAMPLE:
//
//	v := 1
//	IndirectOr(&v, 100)   ⏩ 1
//	IndirectOr(nil, 100)  ⏩ 100
func IndirectOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// IsNull returns whether the given value v is zero value.
// IsNull 判断给定值是否为零值。
func IsNull[T any](p T) bool {
	return reflect.ValueOf(p).IsZero()
}

// IsNil returns whether the given pointer v is nil.
// IsNil 判断给定指针是否为 nil。
func IsNil[T any](p *T) bool {
	return p == nil
}

// IsNotNil is negation of [IsNil].
// IsNotNil 是 [IsNil] 的取反。
func IsNotNil[T any](p *T) bool {
	return p != nil
}

// Clone returns a shallow copy of the slice.
// If the given pointer is nil, nil is returned.
// Clone 返回指向值的浅拷贝；给定指针为 nil 时返回 nil。
//
// HINT: The element is copied using assignment (=), so this is a shallow clone.
// If you want to do a deep clone, use [CloneBy] with an appropriate element
// clone function.
// 提示：元素用赋值（=）拷贝，因此是浅拷贝；需要深拷贝请用 [CloneBy]
// 并提供合适的元素克隆函数。
//
// AKA: Copy
func Clone[T any](p *T) *T {
	if p == nil {
		return nil
	}
	clone := *p
	return &clone
}

// CloneBy is variant of [Clone], it returns a copy of the map.
// Element is copied using function f.
// If the given pointer is nil, nil is returned.
// CloneBy 是 [Clone] 的变体：元素用函数 f 拷贝；给定指针为 nil 时返回 nil。
func CloneBy[T any](p *T, f func(T) T) *T {
	return Map(p, f)
}

// Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
// Equal 在两个指针都为 nil 或都指向相同值时返回 true。
func Equal[T comparable](a, b *T) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return *a == *b
}

// EqualTo returns whether the value of pointer p is equal to value v.
// It a shortcut of "x != nil && *x == y".
// EqualTo 判断指针 p 指向的值是否等于 v，是 "x != nil && *x == y" 的简写。
//
// EXAMPLE:
//
//	x, y := 1, 2
//	EqualTo(&x, 1)   ⏩  true
//	EqualTo(&y, 1)   ⏩  false
//	EqualTo(nil, 1)  ⏩  false
func EqualTo[T comparable](p *T, v T) bool {
	return p != nil && *p == v
}

// Map applies function f to element of pointer p.
// If p is nil, f will not be called and nil is returned, otherwise,
// result of f are returned as a new pointer.
// Map 对指针 p 指向的值应用函数 f：p 为 nil 时不调用 f 并返回 nil，
// 否则把 f 的结果包装为新指针返回。
//
// EXAMPLE:
//
//	i := 1
//	Map(&i, strconv.Itoa)       ⏩  (*string)("1")
//	Map[int](nil, strconv.Itoa) ⏩  (*string)(nil)
func Map[F, T any](p *F, f func(F) T) *T {
	if p == nil {
		return nil
	}
	return new(f(*p))
}

// AllPtrFieldsNil tests whether all pointer fields in a struct are nil.  This is useful when,
// for example, an API struct is handled by plugins which need to distinguish
// "no plugin accepted this spec" from "this spec is empty".
// AllPtrFieldsNil 检查结构体的所有指针字段是否均为 nil。例如插件处理 API 结构体时，
// 需要区分“没有插件接受该 spec”与“该 spec 为空”。
//
// This function is only valid for structs and pointers to structs.  Any other
// type will cause a panic.  Passing a typed nil pointer will return true.
// 本函数只对结构体及指向结构体的指针有效，其他类型会 panic；
// 传入带类型的 nil 指针返回 true。
func AllPtrFieldsNil(obj any) bool {
	v := reflect.ValueOf(obj)
	if !v.IsValid() {
		panic(fmt.Sprintf("reflect.ValueOf() produced a non-valid Value for %#v", obj))
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	for _, field := range v.Fields() {
		if field.Kind() == reflect.Ptr && !field.IsNil() {
			return false
		}
	}
	return true
}
