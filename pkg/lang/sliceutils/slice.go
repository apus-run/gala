package sliceutils

import (
	"fmt"
	"slices"
	"sort"

	"github.com/apus-run/gala/pkg/lang/constraints"
	"github.com/apus-run/gala/pkg/lang/optional/of"
	"github.com/apus-run/gala/pkg/lang/result"
)

// Map applies function f to each element of slice s with type F.
// Results of f are returned as a newly allocated slice with type T.
//
// 🚀 EXAMPLE:
//
//	Map([]int{1, 2, 3}, strconv.Itoa) ⏩ []string{"1", "2", "3"}
//	Map([]int{}, strconv.Itoa)        ⏩ []string{}
//	Map(nil, strconv.Itoa)            ⏩ []string{}
//	Map([]string{"a", "b", "cd"}, func(s string) int {
//	  return len(s)
//	})
//
// 💡 HINT:
//
//   - Use [FilterMap] if you also want to ignore some element during mapping.
//   - Use [TryMap] if function f may fail (return (T, error))
func Map[F, T any](s []F, f func(F) T) []T {
	ret := make([]T, 0, len(s))
	for _, v := range s {
		ret = append(ret, f(v))
	}
	return ret
}

// ToMap collects elements of slice to map, both map keys and values are produced
// by mapping function f.
//
// 🚀 EXAMPLE:
//
//	type Foo struct {
//		ID   int
//		Name string
//	}
//	mapper := func(f Foo) (int, string) { return f.ID, f.Name }
//	ToMap([]Foo{}, mapper) ⏩ map[int]string{}
//	s := []Foo{{1, "one"}, {2, "two"}, {3, "three"}}
//	ToMap(s, mapper)       ⏩ map[int]string{1: "one", 2: "two", 3: "three"}
func ToMap[T, V any, K comparable](s []T, f func(T) (K, V)) map[K]V {
	m := make(map[K]V, len(s))
	for _, e := range s {
		k, v := f(e)
		m[k] = v
	}
	return m
}

// TryMap is a variant of [Map] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	TryMap([]string{"1", "2", "3"}, strconv.Atoi) ⏩ result.OK([]int{1, 2, 3})
//	TryMap([]string{"1", "2", "a"}, strconv.Atoi) ⏩ result.Err("strconv.Atoi: parsing \"a\": invalid syntax")
//	TryMap([]string{}, strconv.Atoi)              ⏩ result.OK([]int{})
//
// 💡 HINT: Use [TryFilterMap] if you want to ignore error during mapping.
func TryMap[F, T any](s []F, f func(F) (T, error)) result.R[[]T] {
	ret := make([]T, 0, len(s))
	for _, v := range s {
		r, err := f(v)
		if err != nil {
			return result.Err[[]T](err)
		}
		ret = append(ret, r)
	}
	return result.OK(ret)
}

// Of creates a slice from variadic arguments.
// If no argument given, an empty (non-nil) slice []T{} is returned.
//
// 💡 HINT: This function is used to omit verbose types like "[]LooooongTypeName{}"
// when constructing slices.
//
// 🚀 EXAMPLE:
//
//	Of(1, 2, 3) ⏩ []int{1, 2, 3}
//	Of(1)       ⏩ []int{1}
//	Of[int]()   ⏩ []int{}
func Of[T any](v ...T) []T {
	if len(v) == 0 {
		return []T{} // never return nil
	}
	return v
}

// Filter applies predicate f to each element of slice s,
// returns those elements that satisfy the predicate f as a newly allocated slice.
//
// 🚀 EXAMPLE:
//
//	Filter([]int{0, 1, 2, 3}, value.IsNotZero[int]) ⏩ []int{1, 2, 3}
//
// 💡 HINT:
//
//   - Use [FilterMap] if you also want to change the element during filtering.
//   - If you need elements that do not satisfy f, use [Reject]
//   - If you need both elements, use [Partition]
func Filter[S ~[]T, T any](s S, f func(T) bool) S {
	ret := make(S, 0, len(s)/2)
	for _, v := range s {
		if f(v) {
			ret = append(ret, v)
		}
	}
	return ret
}

// FilterMap does [Filter] and [Map] at the same time, applies function f to
// each element of slice s. f returns (T, bool):
//
//   - If true ,the return value with type T will added to
//     the result slice []T.
//   - If false, the return value with type T will be dropped.
//
// 🚀 EXAMPLE:
//
//	f := func(i int) (string, bool) { return strconv.Itoa(i), i != 0 }
//	FilterMap([]int{1, 2, 3, 0, 0}, f) ⏩ []string{"1", "2", "3"}
//
// 💡 HINT: Use [TryFilterMap] if function f returns (T, error).
func FilterMap[F, T any](s []F, f func(F) (T, bool)) []T {
	ret := make([]T, 0, len(s))
	for _, v := range s {
		r, ok := f(v)
		if ok {
			ret = append(ret, r)
		}
	}
	return ret
}

// TryFilterMap is a variant of [FilterMap] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	TryFilterMap([]string{"1", "2", "3"}, strconv.Atoi) ⏩ []int{1, 2, 3}
//	TryFilterMap([]string{"1", "2", "a"}, strconv.Atoi) ⏩ []int{1, 2}
func TryFilterMap[F, T any](s []F, f func(F) (T, error)) []T {
	ret := make([]T, 0, len(s)/2)
	for _, v := range s {
		r, err := f(v)
		if err != nil {
			continue // ignored
		}
		ret = append(ret, r)
	}
	return ret
}

// Reject applies predicate f to each element of slice s,
// returns those elements that do not satisfy the predicate f as a newly allocated slice.
//
// 🚀 EXAMPLE:
//
//	Reject([]int{0, 1, 2, 3}, value.IsZero[int]) ⏩ []int{1, 2, 3}
//
// 💡 HINT:
//
//   - If you need elements that satisfy f, use [Filter]
//   - If you need both elements, use [Partition]
func Reject[S ~[]T, T any](s S, f func(T) bool) S {
	ret := make(S, 0, len(s)/2)
	for _, v := range s {
		if !f(v) {
			ret = append(ret, v)
		}
	}
	return ret
}

// Partition applies predicate f to each element of slice s,
// divides elements into 2 parts: satisfy f and do not satisfy f.
//
// 🚀 EXAMPLE:
//
//	Partition([]int{0, 1, 2, 3}, value.IsNotZero[int]) ⏩ []int{1, 2, 3}, []int{0}
//
// 💡 HINT:
//
//   - Use [Filter] or [Reject] if you need only one of the return values
//   - Use [Chunk] or [Divide] if you want to divide elements by index
func Partition[S ~[]T, T any](s S, f func(T) bool) (S, S) {
	var (
		retTrue  = make(S, 0, len(s)/2)
		retFalse = make(S, 0, len(s)/2)
	)
	for _, v := range s {
		if f(v) {
			retTrue = append(retTrue, v)
		} else {
			retFalse = append(retFalse, v)
		}
	}
	return retTrue, retFalse
}

// Contains returns whether the element occur in slice.
//
// 🚀 EXAMPLE:
//
//	Contains([]int{0, 1, 2, 3, 4}, 1) ⏩ true
//	Contains([]int{0, 1, 2, 3, 4}, 5) ⏩ false
//	Contains([]int{}, 5)              ⏩ false
//
// 💡 HINT:
//
//   - Use [ContainsAll], [ContainsAny] if you have multiple values to query
//   - Use [Index] if you also want to know index of the found value
//   - Use [Any] or [Find] if type of v is non-comparable
func Contains[T comparable](s []T, v T) bool {
	for _, vv := range s {
		if v == vv {
			return true
		}
	}
	return false
}

// Select returns a slice containing the elements at the given indices of the input slice.
// CAUTION: This function panics if any index is out of range.
func Select[T any](a []T, indices ...int) []T {
	if len(indices) == 0 {
		return nil
	}
	result := make([]T, 0, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= len(a) {
			panic(fmt.Errorf("invalid index %d: outside of expected range [0, %d)", idx, len(a)))
		}
		result = append(result, a[idx])
	}
	return result
}

// Find returns the possible first element of slice that satisfies predicate f.
//
// 🚀 EXAMPLE:
//
//	s := []int{0, 1, 2, 3, 4}
//	Find(s, func(v int) bool { return v > 0 }) ⏩ of.OK(1)
//	Find(s, func(v int) bool { return v < 0 }) ⏩ of.Nil[int]()
//
// 💡 HINT:
//
//   - Use [Contains] if you just want to know whether the value exists
//   - Use [IndexBy] if you want to know the index of value
//   - Use [FindRev] if you want to find in reverse order
//   - Use [Count] if you want to count the occurrences of element
//
// 💡 AKA: Search
func Find[T any](s []T, f func(T) bool) of.O[T] {
	for _, v := range s {
		if f(v) {
			return of.OK(v)
		}
	}
	return of.Nil[T]()
}

// FindRev is a variant of [Find] in reverse order.
//
// 🚀 EXAMPLE:
//
//	s := []int{0, 1, 2, 3, 4}
//	FindRev(s, func(v int) bool { return v > 0 }) ⏩ of.OK(4)
//	FindRev(s, func(v int) bool { return v < 0 }) ⏩ of.Nil[int]()
func FindRev[T any](s []T, f func(T) bool) of.O[T] {
	for i := len(s) - 1; i >= 0; i-- {
		if f(s[i]) {
			return of.OK(s[i])
		}
	}
	return of.Nil[T]()
}

// GroupBy adjacent elements according to key returned by function f.
//
// 🚀 EXAMPLE:
//
//	GroupBy([]int{1, 2, 3, 4},
//	func(v int) string {
//	    return cond.If(v%2 == 0, "even", "odd")
//	})
//
//	⏩
//
//	map[string][]int{
//	    "odd": {1, 3},
//	    "even": {2, 4},
//	}
//
// 💡 HINT: If function f returns bool, use [Partition] instead.
func GroupBy[S ~[]T, K comparable, T any](s S, f func(T) K) map[K]S {
	if s == nil {
		return nil
	}
	m := make(map[K]S, len(s))
	for i := range s {
		k := f(s[i])
		m[k] = append(m[k], s[i])
	}
	return m
}

// CloneShallow2DSlice clones a 2D slice, creating a new slice and copying the contents of the underlying array.
// If `in` is a nil slice, a nil slice is returned. If `in` is an empty slice, an empty slice is returned.
func CloneShallow2DSlice[T any](in [][]T) [][]T {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return [][]T{}
	}
	out := make([][]T, len(in))
	for idx := range in {
		out[idx] = slices.Clone(in[idx])
	}
	return out
}

// CloneBy is variant of [Clone], it returns a copy of the slice.
// Elements are copied using function clone.
// If the given slice is nil, nil is returned.
//
// 💡 AKA: CopyBy
func CloneBy[S ~[]T, T any](s S, f func(T) T) S {
	if s == nil {
		return nil
	}
	return Map(s, f)
}

// Concat returns a new slice concatenating the passed in slices.
//
// This is directly copied from the implementation of https://pkg.go.dev/slices#Concat
// as of go1.22.2.
//
// This may be removed from the repository once go1.22 becomes the minimum required version.
func Concat[S ~[]E, E any](slcs ...S) S {
	size := 0
	for _, s := range slcs {
		size += len(s)
		if size < 0 {
			panic("len out of range")
		}
	}
	newslice := slices.Grow[S](nil, size)
	for _, s := range slcs {
		newslice = append(newslice, s...)
	}
	return newslice
}

// Diff returns, given two slices a and b sorted according to lessFunc, a slice of the elements occurring in a and b
// only, respectively.
func Diff[T any](slice1, slice2 []T, lessFunc func(a, b T) bool) (aOnly, bOnly []T) {
	var i, j int
	for i < len(slice1) && j < len(slice2) {
		if lessFunc(slice1[i], slice2[j]) {
			aOnly = append(aOnly, slice1[i])
			i++
		} else if lessFunc(slice2[j], slice1[i]) {
			bOnly = append(bOnly, slice2[j])
			j++
		} else { // slice1[i] and slice2[j] are "equal"
			i++
			j++
		}
	}

	aOnly = append(aOnly, slice1[i:]...)
	bOnly = append(bOnly, slice2[j:]...)
	return
}

// Without returns the slice of elements in the first slice that aren't in the second slice.
func Without[T comparable](slice1, slice2 []T) []T {
	if len(slice1) == 0 || len(slice2) == 0 {
		return slice1
	}

	blockedElems := make(map[T]struct{}, len(slice2))
	for _, s := range slice2 {
		blockedElems[s] = struct{}{}
	}
	var newSlice []T
	for _, s := range slice1 {
		if _, ok := blockedElems[s]; !ok {
			newSlice = append(newSlice, s)
			blockedElems[s] = struct{}{}
		}
	}
	return newSlice
}

// Reversed returns a slice that contains the elements of the input slice in reverse order.
func Reversed[T any](slice []T) []T {
	cloned := slices.Clone(slice)
	slices.Reverse(cloned)
	return cloned
}

// Count returns the times of value v that occur in slice s.
//
// 🚀 EXAMPLE:
//
//	Count([]string{"a", "b", "c"}, "a") ⏩ 1
//	Count([]int{0, 1, 2, 0, 5, 3}, 0)   ⏩ 2
//
// 💡 HINT:
//
//   - Use [Contains] if you just want to know whether the element exitss or not
//   - Use [CountBy] if type of v is non-comparable
func Count[T comparable](s []T, v T) int {
	var count int
	for i := range s {
		if s[i] == v {
			count++
		}
	}
	return count
}

// CountBy returns the times of element in slice s that satisfy the predicate f.
//
// 🚀 EXAMPLE:
//
//	CountBy([]string{"a", "b", "c"}, func (v string) bool { return v < "b" }) ⏩ 1
//	CountBy([]int{0, 1, 2, 3, 4}, func (v int) bool { return v % i == 0 })    ⏩ 3
//
// 💡 HINT: Use [Any] if you just want to know whether at least one element satisfies predicate f.
func CountBy[T any](s []T, f func(T) bool) int {
	var count int
	for i := range s {
		if f(s[i]) {
			count++
		}
	}
	return count
}

// CountValues returns the occurrences of each element in slice s.
//
// 🚀 EXAMPLE:
//
//	CountValues([]string{"a", "b", "b"}) ⏩ map[string]int{"a": 1, "b": 2}
//	CountValues([]int{0, 1, 2, 0, 1, 1}) ⏩ map[int]int{0: 2, 1: 3, 2: 1}
//
// 💡 HINT:
//
//   - Use [CountValuesBy] if the element in slice s is non-comparable
func CountValues[T comparable](s []T) map[T]int {
	ret := make(map[T]int, len(s)/2)
	for i := range s {
		ret[s[i]]++
	}
	return ret
}

// CountValuesBy returns the times of each element in slice s that satisfy the predicate f.
//
// 🚀 EXAMPLE:
//
//	CountValuesBy([]int{0, 1, 2, 3, 4}, func(v int) bool { return v%2 == 0 }) ⏩ map[bool]int{true: 3, false: 2}
//	type Foo struct{ v int }
//	foos := []Foo{{1}, {2}, {3}}
//	CountValuesBy(foos, func(v Foo) bool { return v.v%2 == 0 }) ⏩ map[bool]int{true: 1, false: 2}
func CountValuesBy[K comparable, T any](s []T, f func(T) K) map[K]int {
	ret := make(map[K]int, len(s)/2)
	for i := range s {
		ret[f(s[i])]++
	}
	return ret
}

type naturallySortableSlice[T constraints.Ordered] []T

func (s naturallySortableSlice[T]) Len() int {
	return len(s)
}

func (s naturallySortableSlice[T]) Less(i, j int) bool {
	return s[i] < s[j]
}

func (s naturallySortableSlice[T]) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// NaturalSort sorts the given slice according to the natural ording of elements.
func NaturalSort[T constraints.Ordered](slice []T) {
	sort.Sort(naturallySortableSlice[T](slice))
}

// StringSlice returns a sorted string slice from the given T.
func StringSlice[T fmt.Stringer](in ...T) []string {
	res := make([]string, 0, len(in))
	for _, i := range in {
		res = append(res, i.String())
	}

	slices.Sort(res)
	return res
}

// FromStringSlice returns a slice T from the given strings.
// Note that this only works for types whose underlying type is string.
func FromStringSlice[T ~string](in ...string) []T {
	res := make([]T, 0, len(in))
	for _, i := range in {
		res = append(res, T(i))
	}
	return res
}

// Unique returns a new slice that contains only the first occurrence of each element in slice.
// Example: Unique([]string{"a", "a", b"}) will return []string{"a", "b"}
func Unique[T comparable](slice []T) []T {
	if slice == nil {
		return nil
	}
	out := make([]T, 0, len(slice))

	seenElems := make(map[T]struct{})
	for _, elem := range slice {
		preNumElems := len(seenElems)
		seenElems[elem] = struct{}{}
		if len(seenElems) == preNumElems { // not added
			continue
		}
		out = append(out, elem)
	}
	return out
}

// Transform returns a new slice that contains the result of applying fn to each element in slice.
func Transform[A, B any](src []A, fn func(A) B) []B {
	if src == nil {
		return nil
	}

	dst := make([]B, 0, len(src))
	for _, a := range src {
		dst = append(dst, fn(a))
	}

	return dst
}

// TransformWithErrorCheck returns a new slice that contains the result of applying fn to each element in slice.
// If any error occurs, it will be returned immediately.
func TransformWithErrorCheck[A, B any](src []A, fn func(A) (B, error)) ([]B, error) {
	if src == nil {
		return nil, nil
	}

	dst := make([]B, 0, len(src))
	for _, a := range src {
		item, err := fn(a)
		if err != nil {
			return nil, err
		}
		dst = append(dst, item)
	}

	return dst, nil
}
