// Pointer
// This package provides some functions for pointer-based operations.

package ptr

import (
	"fmt"
	"reflect"
)

// Of returns a pointer that points to equivalent value of value v.
// (T → *T).
// It is useful when you want to "convert" a unaddressable value to pointer.
//
// If you need to assign the address of a literal to a pointer:
//
//	 payload := struct {
//		    Name *string
//	 }
//
// The practice without generic:
//
//	x := "name"
//	payload.Name = &x
//
// Use generic:
//
//	payload.Name = Of("name")
//
// 💡 HINT: use [Indirect] to dereference pointer (*T → T).
//
// ⚠️  WARNING: The returned pointer does not point to the original value because
// Go is always pass by value, user CAN NOT modify the value by modifying the pointer.
func Of[T any](v T) *T {
	return &v
}

// ToPtr returns a pointer to the given value.
func ToPtr[T any](v T) *T {
	return &v
}

// From returns the value pointed to by the pointer p.
// If the pointer is nil, returns the zero value of T instead.
func From[T any](v *T) T {
	var zero T
	if v != nil {
		return *v
	}

	return zero
}

// FromOrDefault dereferences ptr and returns the value it points to if no nil, or else
// returns def.
func FromOrDefault[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}

type Integer interface {
	~int64 | ~int32 | ~int16 | ~int8 | ~int
}

func ConvIntPtr[T, K Integer](p *T) *K {
	if p == nil {
		return nil
	}
	return Of((K)(*p))
}

// Indirect returns the value pointed to by the pointer p.
// If the pointer is nil, returns the zero value of T instead.
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
//
// 💡 AKA: Unref, Unreference, Deref, Dereference
func Indirect[T any](p *T) (v T) {
	if p == nil {
		// Explicitly return value.Zero causes an extra copy.
		// return value.Zero[T]()
		return // the initial value is zero value, see also [Indirect_valueZero].
	}
	return *p
}

// IndirectOr is a variant of [Indirect],
// If the pointer is nil, returns the fallback value instead.
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
func IsNull[T any](p T) bool {
	return reflect.ValueOf(p).IsZero()
}

// IsNil returns whether the given pointer v is nil.
func IsNil[T any](p *T) bool {
	return p == nil
}

// IsNotNil is negation of [IsNil].
func IsNotNil[T any](p *T) bool {
	return p != nil
}

// Clone returns a shallow copy of the slice.
// If the given pointer is nil, nil is returned.
//
// HINT: The element is copied using assignment (=), so this is a shallow clone.
// If you want to do a deep clone, use [CloneBy] with an appropriate element
// clone function.
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
func CloneBy[T any](p *T, f func(T) T) *T {
	return Map(p, f)
}

// Equal returns true if both arguments are nil or both arguments
// dereference to the same value.
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
//
// EXAMPLE:
//
//	x, y := 1, 2
//	Equal(&x, 1)   ⏩  true
//	Equal(&y, 1)   ⏩n false
//	Equal(nil, 1)  ⏩  false
func EqualTo[T comparable](p *T, v T) bool {
	return p != nil && *p == v
}

// Map applies function f to element of pointer p.
// If p is nil, f will not be called and nil is returned, otherwise,
// result of f are returned as a new pointer.
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
	return ToPtr(f(*p))
}

// AllPtrFieldsNil tests whether all pointer fields in a struct are nil.  This is useful when,
// for example, an API struct is handled by plugins which need to distinguish
// "no plugin accepted this spec" from "this spec is empty".
//
// This function is only valid for structs and pointers to structs.  Any other
// type will cause a panic.  Passing a typed nil pointer will return true.
func AllPtrFieldsNil(obj interface{}) bool {
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
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.Ptr && !v.Field(i).IsNil() {
			return false
		}
	}
	return true
}
