package maputils

import (
	"github.com/apus-run/gala/pkg/lang/optional/of"
	"github.com/apus-run/gala/pkg/lang/ptr"
	"github.com/apus-run/gala/pkg/lang/result"
	"github.com/apus-run/gala/pkg/lang/sliceutils"
	"github.com/apus-run/gala/pkg/lang/value"
)

// ToAnyValue converts a map[K]V to map[K]any.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	ToAnyValue(m) ⏩ map[int]any{1: 1, 2: 2}
func ToAnyValue[K comparable, V any](m map[K]V) map[K]any {
	if m == nil {
		return nil
	}
	n := make(map[K]any, len(m))
	for k, v := range m {
		n[k] = v
	}

	return n
}

// TransformKey transforms the keys of map m using function f.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	TransformKey(m, func(k int) int { return k * 2 }) ⏩ map[int]int{2: 1, 4: 2}
func TransformKey[K1, K2 comparable, V any](m map[K1]V, f func(K1) K2) map[K2]V {
	if m == nil {
		return nil
	}
	n := make(map[K2]V, len(m))
	for k1, v := range m {
		n[f(k1)] = v
	}
	return n
}

// Concat returns the unions of maps as a new map.
//
// 💡 NOTE:
//
//   - Once the key conflicts, the newer value always replace the older one ([DiscardOld]),
//   - If the result is an empty set, always return an empty map instead of nil
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	Concat(m, nil)             ⏩ map[int]int{1: 1, 2: 2}
//	Concat(m, map[int]{3: 3})  ⏩ map[int]int{1: 1, 2: 2, 3: 3}
//	Concat(m, map[int]{2: -1}) ⏩ map[int]int{1: 1, 2: -1} // "2:2" is replaced by the newer "2:-1"
//
// 💡 AKA: Merge, Union, Combine
func Concat[K comparable, V any](ms ...map[K]V) map[K]V {
	// FastPath: no map or only one map given.
	if len(ms) == 0 {
		return make(map[K]V)
	}
	if len(ms) == 1 {
		return cloneWithoutNilCheck(ms[0])
	}

	var maxLen int
	for _, m := range ms {
		if len(m) > maxLen {
			maxLen = len(m)
		}
	}
	ret := make(map[K]V, maxLen)
	// FastPath: all maps are empty.
	if maxLen == 0 {
		return ret
	}

	// Concat all maps.
	for _, m := range ms {
		for k, v := range m {
			ret[k] = v
		}
	}
	return ret
}

// Map applies function f to each key and value of map m.
// Results of f are returned as a new map.
//
// 🚀 EXAMPLE:
//
//	f := func(k, v int) (string, string) { return strconv.Itoa(k), strconv.Itoa(v) }
//	Map(map[int]int{1: 1}, f) ⏩ map[string]string{"1": "1"}
//	Map(map[int]int{}, f)     ⏩ map[string]string{}
func Map[K1, K2 comparable, V1, V2 any](m map[K1]V1, f func(K1, V1) (K2, V2)) map[K2]V2 {
	r := make(map[K2]V2, len(m))
	for k, v := range m {
		k2, v2 := f(k, v)
		r[k2] = v2
	}
	return r
}

// TryMap is a variant of [Map] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	f := func(k, v int) (string, string, error) {
//		ki, kerr := strconv.Atoi(k)
//		vi, verr := strconv.Atoi(v)
//		return ki, vi, errors.Join(kerr, verr)
//	}
//	TryMap(map[string]string{"1": "1"}, f) ⏩ result.OK(map[int]int{1: 1})
//	TryMap(map[string]string{"1": "a"}, f) ⏩ result.Err("strconv.Atoi: parsing \"a\": invalid syntax")
//
// 💡 HINT:
//
//   - Use [TryFilterMap] if you want to ignore error during mapping.
//   - Use [TryMapKeys] if you only need to map the keys.
//   - Use [TryMapValues] if you only need to map the values.
func TryMap[K1, K2 comparable, V1, V2 any](m map[K1]V1, f func(K1, V1) (K2, V2, error)) result.R[map[K2]V2] {
	r := make(map[K2]V2, len(m))
	for k, v := range m {
		k2, v2, err := f(k, v)
		if err != nil {
			return result.Err[map[K2]V2](err)
		}
		r[k2] = v2
	}
	return result.OK(r)
}

// MapKeys is a variant of [Map], applies function f to each key of map m.
// Results of f and the corresponding values are returned as a new map.
//
// 🚀 EXAMPLE:
//
//	MapKeys(map[int]int{1: 1}, strconv.Itoa) ⏩ map[string]int{"1": 1}
//	MapKeys(map[int]int{}, strconv.Itoa)     ⏩ map[string]int{}
func MapKeys[K1, K2 comparable, V any](m map[K1]V, f func(K1) K2) map[K2]V {
	r := make(map[K2]V, len(m))
	for k, v := range m {
		r[f(k)] = v
	}
	return r
}

// TryMapKeys is a variant of [MapKeys] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	TryMapKeys(map[string]string{"1": "1"}, strconv.Atoi) ⏩ result.OK(map[int]string{1: "1"})
//	TryMapKeys(map[string]string{"a": "1"}, strconv.Atoi) ⏩ result.Err("strconv.Atoi: parsing \"a\": invalid syntax")
//	TryMapKeys(map[string]string{}, strconv.Itoa)         ⏩ result.OK(map[int]string{})
func TryMapKeys[K1, K2 comparable, V any](m map[K1]V, f func(K1) (K2, error)) result.R[map[K2]V] {
	r := make(map[K2]V, len(m))
	for k, v := range m {
		k2, err := f(k)
		if err != nil {
			return result.Err[map[K2]V](err)
		}
		r[k2] = v
	}
	return result.OK(r)
}

// MapValues is a variant of [Map], applies function f to each values of map m.
// Results of f and the corresponding keys are returned as a new map.
//
// 🚀 EXAMPLE:
//
//	MapValues(map[int]int{1: 1}, strconv.Itoa) ⏩ map[int]string{1: "1"}
//	MapValues(map[int]int{}, strconv.Itoa)     ⏩ map[int]string{}
func MapValues[K comparable, V1, V2 any](m map[K]V1, f func(V1) V2) map[K]V2 {
	r := make(map[K]V2, len(m))
	for k, v := range m {
		r[k] = f(v)
	}
	return r
}

// TryMapValues is a variant of [MapValues] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	TryMapValues(map[string]string{"1": "1"}, strconv.Atoi) ⏩ result.OK(map[string]int{"1": 1})
//	TryMapValues(map[string]string{"1": "a"}, strconv.Atoi) ⏩ result.Err("strconv.Atoi: parsing \"a\": invalid syntax")
//	TryMapValues(map[string]string{}, strconv.Itoa)         ⏩ result.OK(map[string]int{})
func TryMapValues[K comparable, V1, V2 any](m map[K]V1, f func(V1) (V2, error)) result.R[map[K]V2] {
	r := make(map[K]V2, len(m))
	for k, v := range m {
		v2, err := f(v)
		if err != nil {
			return result.Err[map[K]V2](err)
		}
		r[k] = v2
	}
	return result.OK(r)
}

// Filter applies predicate f to each key and value of map m,
// returns those keys and values that satisfy the predicate f as a new map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 2, 4: 3}
//	pred := func(k, v int) bool { return (k+v)%2 == 0 }
//	Filter(m, pred) ⏩ map[int]int{1: 1, 2: 2}
//
// 💡 HINT:
//
//   - Use [FilterKeys] if you only need to filter the keys.
//   - Use [FilterValues] if you only need to filter the values.
//   - Use [FilterMap] if you also want to modify the keys/values during filtering.
func Filter[M ~map[K]V, K comparable, V any](m M, f func(K, V) bool) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if f(k, v) {
			r[k] = v
		}
	}
	return r
}

// FilterKeys is a variant of [Filter], applies predicate f to each key of map m,
// returns keys that satisfy the predicate f and the corresponding values as a new map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 2, 4: 3}
//	pred := func(k int) bool { return k%2 == 0 }
//	FilterKeys(m, pred) ⏩ map[int]int{2: 2, 4: 3}
func FilterKeys[M ~map[K]V, K comparable, V any](m M, f func(K) bool) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if f(k) {
			r[k] = v
		}
	}
	return r
}

// FilterByKeys is a variant of [Filter], filters map m by given keys slice,
// returns a new map containing only the key-value pairs where the key exists in the keys slice.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}
//	keys := []int{1, 3, 5}
//	FilterByKeys(m, keys) ⏩ map[int]int{1: 1, 3: 3}
func FilterByKeys[M ~map[K]V, K comparable, V any](m M, keys ...K) M {
	r := make(M, value.Min(len(keys), len(m)))
	for _, key := range keys {
		if v, ok := m[key]; ok {
			r[key] = v
		}
	}
	return r
}

// FilterValues is a variant of [Filter], applies predicate f to each value of map m,
// returns values that satisfy the predicate f and the corresponding keys as a new map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 2, 4: 3}
//	pred := func(v int) bool { return v%2 == 0 }
//	FilterValues(m, pred) ⏩ map[int]int{2: 2, 3: 2}
func FilterValues[M ~map[K]V, K comparable, V any](m M, f func(V) bool) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if f(v) {
			r[k] = v
		}
	}
	return r
}

// FilterByValues is a variant of [Filter], filters map m by given values slice,
// returns a new map containing only the key-value pairs where the value exists in the values slice.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 10, 2: 20, 3: 10, 4: 30}
//	values := []int{10, 30}
//	FilterByValues(m, values) ⏩ map[int]int{1: 10, 3: 10, 4: 30}
func FilterByValues[M ~map[K]V, K, V comparable](m M, values ...V) M {
	r := make(M, value.Min(len(values), len(m)))
	for k, v := range m {
		if sliceutils.Contains(values, v) {
			r[k] = v
		}
	}
	return r
}

// Reject applies predicate f to each key and value of map m,
// returns those keys and values that do not satisfy the predicate f as a new map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 2, 4: 3}
//	pred := func(k, v int) bool { return (k+v)%2 != 0 }
//	Reject(m, pred) ⏩ map[int]int{1: 1, 2: 2}
//
// 💡 HINT:
//
//   - Use [RejectKeys] if you only need to reject the keys.
//   - Use [RejectValues] if you only need to reject the values.
func Reject[M ~map[K]V, K comparable, V any](m M, f func(K, V) bool) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if !f(k, v) {
			r[k] = v
		}
	}
	return r
}

// RejectKeys applies predicate f to each key of map m,
// returns keys that do not satisfy the predicate f and the corresponding values as a new map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 2, 4: 3}
//	pred := func(k int) bool { return k%2 != 0 }
//	RejectKeys(m, pred) ⏩ map[int]int{2: 2, 4: 3}
func RejectKeys[M ~map[K]V, K comparable, V any](m M, f func(K) bool) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if !f(k) {
			r[k] = v
		}
	}
	return r
}

// RejectByKeys is the opposite of [FilterByKeys], removes entries from map m where the key exists in the keys slice,
// returns a new map containing only the key-value pairs where the key does not exist in the keys slice.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}
//	keys := []int{1, 3}
//	RejectByKeys(m, keys) ⏩ map[int]int{2: 2, 4: 4}
func RejectByKeys[M ~map[K]V, K comparable, V any](m M, keys ...K) M {
	r := Clone(m)
	for _, key := range keys {
		delete(r, key)
	}
	return r
}

// RejectValues applies predicate f to each value of map m,
// returns values that do not satisfy the predicate f and the corresponding keys as a new map.
//
// 🚀 EXAMPLE:
//
//	 m := map[int]int{1: 1, 2: 2, 3: 2, 4: 3}
//	 pred := func(v int) bool { return v%2 != 0 }
//		RejectValues(m, pred) ⏩ map[int]int{2: 2, 3: 2}
func RejectValues[M ~map[K]V, K comparable, V any](m M, f func(V) bool) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if !f(v) {
			r[k] = v
		}
	}
	return r
}

// RejectByValues is the opposite of [FilterByValues], removes entries from map m where the value exists in the values slice,
// returns a new map containing only the key-value pairs where the value does not exist in the values slice.
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 10, 2: 20, 3: 10, 4: 30}
//	values := []int{10, 30}
//	RejectByValues(m, values) ⏩ map[int]int{2: 20}
func RejectByValues[M ~map[K]V, K, V comparable](m M, values ...V) M {
	r := make(M, len(m)/2)
	for k, v := range m {
		if !sliceutils.Contains(values, v) {
			r[k] = v
		}
	}
	return r
}

// Values returns the values of the map m.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}
//	Values(m) ⏩ []string{"1", "4", "2", "3"} //⚠️INDETERMINATE ORDER⚠️
//
// ⚠️  WARNING: The keys values be in an indeterminate order,
func Values[K comparable, V any](m map[K]V) []V {
	r := make([]V, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}

// Clone returns a shallow copy of map.
// If the given map is nil, nil is returned.
//
// 🚀 EXAMPLE:
//
//	Clone(map[int]int{1: 1, 2: 2}) ⏩ map[int]int{1: 1, 2: 2}
//	Clone(map[int]int{})           ⏩ map[int]int{}
//	Clone[int, int](nil)           ⏩ nil
//
// 💡 HINT: Both keys and values are copied using assignment (=), so this is a shallow clone.
// 💡 AKA: Copy
func Clone[K comparable, V any, M ~map[K]V](m M) M {
	if m == nil {
		return nil
	}
	return cloneWithoutNilCheck(m)
}

func cloneWithoutNilCheck[K comparable, V any, M ~map[K]V](m M) M {
	r := make(M, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}

// TODO: Unhidden Fold/Reduce funcs
//
// Fold applies function f cumulatively to each key and value of map m,
// so as to fold the map to a single value.
//
//	Fold(map[int]int{1: 1, 2: 2}, func(acc, k, v int) int { return acc + k + v }, 0) ⏩ 6
func Fold[K comparable, V, T any](m map[K]V, f func(T, K, V) T, init T) T {
	acc := init
	for k, v := range m {
		acc = f(acc, k, v)
	}
	return acc
}

// FoldKeys applies function f cumulatively to each key of map m,
// so as to fold the keys of map to a single value.
func FoldKeys[K comparable, V, T any](m map[K]V, f func(T, K) T, init T) T {
	acc := init
	for k := range m {
		acc = f(acc, k)
	}
	return acc
}

// FoldValues applies function f cumulatively to each value of map m,
// so as to fold the values of map to a single value.
func FoldValues[K comparable, V, T any](m map[K]V, f func(T, V) T, init T) T {
	acc := init
	for _, v := range m {
		acc = f(acc, v)
	}
	return acc
}

// Merge is alias of [Union].
func Merge[M ~map[K]V, K comparable, V any](ms ...M) M {
	return Union(ms...)
}

// Union returns the unions of maps as a new map.
//
// 💡 NOTE:
//
//   - Once the key conflicts, the newer value always replace the older one ([DiscardOld]),
//     use [UnionBy] and [ConflictFunc] to customize conflict resolution.
//   - If the result is an empty set, always return an empty map instead of nil
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	Union(m, nil)             ⏩ map[int]int{1: 1, 2: 2}
//	Union(m, map[int]{3: 3})  ⏩ map[int]int{1: 1, 2: 2, 3: 3}
//	Union(m, map[int]{2: -1}) ⏩ map[int]int{1: 1, 2: -1} // "2:2" is replaced by the newer "2:-1"
//
// 💡 AKA: Merge, Concat, Combine
func Union[M ~map[K]V, K comparable, V any](ms ...M) M {
	// Fastpath: no map or only one map given.
	if len(ms) == 0 {
		return make(M)
	}
	if len(ms) == 1 {
		return cloneWithoutNilCheck(ms[0])
	}

	var maxLen int
	for _, m := range ms {
		maxLen = value.Max(maxLen, len(m))
	}
	ret := make(M, maxLen)
	// Fastpath: all maps are empty.
	if maxLen == 0 {
		return ret
	}

	// Union all maps.
	for _, m := range ms {
		for k, v := range m {
			ret[k] = v
		}
	}
	return ret
}

type ConflictFunc[K comparable, V any] func(key K, oldVal, newVal V) V

// UnionBy returns the unions of maps as a new map, conflicts are resolved by a
// custom [ConflictFunc].
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	Union(m, map[int]{2: 0})                               ⏩ map[int]int{1: 1, 2: 0} // "2:2" is replaced by the newer "2:0"
//	UnionBy(gslice.Of(m, map[int]int{2: 0}), DiscardOld()) ⏩ map[int]int{1: 1, 2: 0} // same as above
//	UnionBy(gslice.Of(m, map[int]int{2: 0}), DiscardNew()) ⏩ map[int]int{1: 1, 2: 2} // "2:2" is kept because it is older
//

// For more examples, see [ConflictFunc].
func UnionBy[M ~map[K]V, K comparable, V any](ms []M, onConflict ConflictFunc[K, V]) M {
	// Fastpath: no map or only one map given.
	if len(ms) == 0 {
		return make(M)
	}
	if len(ms) == 1 {
		return cloneWithoutNilCheck(ms[0])
	}

	var maxLen int
	for _, m := range ms {
		maxLen = value.Max(maxLen, len(m))
	}
	ret := make(M, maxLen)
	// Fastpath: all maps are empty.
	if maxLen == 0 {
		return ret
	}

	// Union all maps with ConflictFunc.
	for _, m := range ms {
		for k, newV := range m {
			if oldV, ok := ret[k]; ok {
				ret[k] = onConflict(k, oldV, newV)
			} else {
				ret[k] = newV
			}
		}
	}
	return ret
}

// Count returns the times of value v that occur in map m.
//
// 🚀 EXAMPLE:
//
//	Count(map[int]string{1: "a", 2: "a", 3: "b"}, "a") ⏩ 2
//
// 💡 HINT:
//
//   - Use [CountValueBy] if type of v is non-comparable
//   - Use [CountBy] if you need to consider key when counting
func Count[K, V comparable](m map[K]V, v V) int {
	var count int
	for _, vv := range m {
		if vv == v {
			count++
		}
	}
	return count
}

// CountBy returns the times of pair (k, v) in map m that satisfy the predicate f.
//
// 🚀 EXAMPLE:
//
//	f := func (k int, v string) bool {
//		i, _ := strconv.Atoi(v)
//		return k%2 == 1 && i%2 == 1
//	}
//	CountBy(map[int]string{1: "1", 2: "2", 3: "3"}, f) ⏩ 0
//	CountBy(map[int]string{1: "1", 2: "2", 3: "4"}, f) ⏩ 1
func CountBy[K comparable, V any](m map[K]V, f func(K, V) bool) int {
	var count int
	for k, v := range m {
		if f(k, v) {
			count++
		}
	}
	return count
}

// CountValueBy returns the times of value v in map m that satisfy the predicate f.
//
// 🚀 EXAMPLE:
//
//	f := func (v string) bool {
//		i, _ := strconv.Atoi(v)
//		return i%2 == 1
//	}
//	CountValueBy(map[int]string{1: "1", 2: "2", 3: "3"}, f) ⏩ 2
//	CountValueBy(map[int]string{1: "1", 2: "2", 3: "4"}, f) ⏩ 1
func CountValueBy[K comparable, V any](m map[K]V, f func(V) bool) int {
	var count int
	for _, v := range m {
		if f(v) {
			count++
		}
	}
	return count
}

// Contains returns whether the key occur in map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: ""}
//	Contains(m, 1)             ⏩ true
//	Contains(m, 0)             ⏩ false
//	var nilMap map[int]string
//	Contains(nilMap, 0)        ⏩ false
//
// 💡 HINT: See also [ContainsAll], [ContainsAny] if you have multiple values to
// query.
func Contains[K comparable, V any](m map[K]V, k K) bool {
	if m == nil || len(m) == 0 {
		return false
	}
	_, ok := m[k]
	return ok
}

// ContainsAny returns whether any of given keys occur in map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "", 2: ""}
//	ContainsAny(m, 1, 2) ⏩ true
//	ContainsAny(m, 1, 3) ⏩ true
//	ContainsAny(m, 3)    ⏩ false
func ContainsAny[K comparable, V any](m map[K]V, ks ...K) bool {
	if m == nil || len(m) == 0 {
		return false
	}
	for _, k := range ks {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// ContainsAll returns whether all of given keys occur in map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "", 2: "",}
//	ContainsAll(m, 1, 2) ⏩ true
//	ContainsAll(m, 1, 3) ⏩ false
//	ContainsAll(m, 3)    ⏩ false
func ContainsAll[K comparable, V any](m map[K]V, ks ...K) bool {
	if (m == nil || len(m) == 0) && len(ks) != 0 {
		return false
	}
	for _, k := range ks {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// Invert inverts the keys and values of map, and returns a new map.
// (map[K]V] → map[V]K).
//
// ⚠️ WARNING: The iteration of the map is in an indeterminate order,
// for multiple KV-pairs with the same V, the K retained after inversion is UNSTABLE.
// If the length of the returned map is equal to the length of the given map,
// there are no key conflicts.
// Use [InvertBy] and [ConflictFunc] to customize conflict resolution.
// Use [InvertGroup] to avoid key loss when multiple keys mapped to the same value.
//
// 🚀 EXAMPLE:
//
//	Invert(map[string]int{"1": 1, "2": 2}) ⏩ map[int]string{1: "1", 2: "2"},
//	Invert(map[string]int{"1": 1, "2": 1}) ⏩ ⚠️ UNSTABLE: map[int]string{1: "1"} or map[int]string{1: "2"}
//
// 💡 AKA: Reverse
func Invert[K, V comparable](m map[K]V) map[V]K {
	r := make(map[V]K)
	for k, v := range m {
		r[v] = k
	}
	return r
}

// InvertBy inverts the keys and values of map, and returns a new map.
// (map[K]V] → map[V]K), conflicts are resolved by a custom [ConflictFunc].
//
// 💡 NOTE: the "oldVal", and "newVal" naming of [ConflictFunc] are meaningless
// because of the map's indeterminate iteration order. Further,
// [DiscardOld] and [DiscardNew] are also meaningless.
//
// 🚀 EXAMPLE:
//
//	InvertBy(map[string]int{"1": 1, "": 1}, DiscardZero(nil) ⏩ map[int]string{1: "1"},
func InvertBy[K, V comparable](m map[K]V, onConflict ConflictFunc[V, K]) map[V]K {
	r := make(map[V]K)
	for k, v := range m {
		if oldKey, ok := r[v]; ok {
			r[v] = onConflict(v, oldKey, k)
		} else {
			r[v] = k
		}
	}
	return r
}

// InvertGroup inverts the map by grouping keys that mapped to the same value into a slice.
// (map[K]V] → map[V][]K).
//
// ⚠️ WARNING: The iteration of the map is in an indeterminate order,
// for multiple KV-pairs with the same V, the order of K in the slice is UNSTABLE.
//
// 🚀 EXAMPLE:
//
//	InvertGroup(map[string]int{"1": 1, "2": 2}) ⏩ map[int][]string{1: {"1"}, 2: {"2"}},
//	InvertGroup(map[string]int{"1": 1, "2": 1}) ⏩ ⚠️ UNSTABLE: map[int][]string{1: {"1", "2"}} or map[int]string{1: {"2", "1"}}
func InvertGroup[K, V comparable](m map[K]V) map[V][]K {
	r := make(map[V][]K)
	for k, v := range m {
		r[v] = append(r[v], k)
	}
	return r
}

// Equal reports whether two maps contain the same key/value pairs.
// values are compared using ==.
//
// 🚀 EXAMPLE:
//
//	Equal(map[int]int{1: 1, 2: 2}, map[int]int{1: 1, 2: 2}) ⏩ true
//	Equal(map[int]int{1: 1}, map[int]int{1: 1, 2: 2})       ⏩ false
//	Equal(map[int]int{}, map[int]int{})                     ⏩ true
//	Equal(map[int]int{}, nil)                               ⏩ true
func Equal[K, V comparable](m1, m2 map[K]V) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v1 := range m1 {
		if v2, ok := m2[k]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}

// EqualBy reports whether two maps contain the same key/value pairs.
// values are compared using function eq.
//
// 🚀 EXAMPLE:
//
//	eq := value.Equal[int]
//	EqualBy(map[int]int{1: 1, 2: 2}, map[int]int{1: 1, 2: 2}, eq) ⏩ true
//	EqualBy(map[int]int{1: 1}, map[int]int{1: 1, 2: 2}, eq)       ⏩ false
//	EqualBy(map[int]int{}, map[int]int{}, eq)                     ⏩ true
//	EqualBy(map[int]int{}, nil, eq)                               ⏩ true
func EqualBy[K comparable, V any](m1, m2 map[K]V, eq func(v1, v2 V) bool) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v1 := range m1 {
		if v2, ok := m2[k]; !ok || !eq(v1, v2) {
			return false
		}
	}
	return true
}

// Diff returns the difference of map m against other maps as a new map.
//
// 💡 NOTE: If the result is an empty set, always return an empty map instead of nil
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	Diff(m, nil)             ⏩ map[int]int{1: 1, 2: 2}
//	Diff(m, map[int]{1: 1})  ⏩ map[int]int{2: 2}
//	Diff(m, map[int]{3: 3})  ⏩ map[int]int{1: 1, 2: 2}
//
// 💡 HINT: Use [github.com/apus-run/gala/pkg/lang/collection/set.Set] if you need a
// set data structure
//
// TODO: Value type of againsts can be diff from m.
func Diff[M ~map[K]V, K comparable, V any](m M, againsts ...M) M {
	if len(m) == 0 {
		return make(M)
	}
	if len(againsts) == 0 {
		return cloneWithoutNilCheck(m)
	}
	ret := make(M, len(m)/2)
	for k, v := range m {
		var found bool
		for _, a := range againsts {
			if _, found = a[k]; found {
				break
			}
		}
		if !found {
			ret[k] = v
		}
	}
	return ret
}

// Intersect returns the intersection of maps as a new map.
//
// 💡 NOTE:
//
//   - Once the key conflicts, the newer one will replace the older one ([DiscardOld]),
//     use [IntersectBy] and [ConflictFunc] to customize conflict resolution.
//   - If the result is an empty set, always return an empty map instead of nil
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	Intersect(m, nil)             ⏩ map[int]int{}
//	Intersect(m, map[int]{3: 3})  ⏩ map[int]int{}
//	Intersect(m, map[int]{1: 1})  ⏩ map[int]int{1: 1}
//	Intersect(m, map[int]{1: -1}) ⏩ map[int]int{1: -1} // "1:1" is replaced by the newer "1:-1"
//
// 💡 HINT: Use [github.com/apus-run/gala/pkg/lang/collection/set.Set] if you need a
// set data structure
func Intersect[M ~map[K]V, K comparable, V any](ms ...M) M {
	// Fastpath: no map or only one map given.
	if len(ms) == 0 {
		return make(M)
	}
	if len(ms) == 1 {
		return cloneWithoutNilCheck(ms[0])
	}

	minLen := len(ms[0])
	for _, m := range ms[1:] {
		minLen = value.Min(minLen, len(m))
	}
	ret := make(M, minLen)
	// Fastpath: all maps are empty.
	if minLen == 0 {
		return ret
	}

	// Intersect all maps.
	for k, v := range ms[0] {
		found := true // at least we found it in ms[0]
		for _, m := range ms[1:] {
			if v, found = m[k]; !found {
				break
			}
		}
		if found {
			ret[k] = v
		}
	}
	return ret
}

// IntersectBy returns the intersection of maps as a new map, conflicts are resolved by a
// custom [ConflictFunc].
//
// 🚀 EXAMPLE:
//
//	m := map[int]int{1: 1, 2: 2}
//	Intersect(m, map[int]{1: -1})                                     ⏩ map[int]int{1: -1} // "1:1" is replaced by the newer "1:-1"
//	IntersectBy(gslice.Of(m, map[int]{1: -1}), DiscardOld[int,int]()) ⏩ map[int]int{1: -1} // same as above
//	IntersectBy(gslice.Of(m, map[int]{1: -1}), DiscardNew[int,int]()) ⏩ map[int]int{1: 1} // "1:1" is kept because it is older
//
// For more examples, see [ConflictFunc].
func IntersectBy[M ~map[K]V, K comparable, V any](ms []M, onConflict ConflictFunc[K, V]) M {
	if len(ms) == 0 {
		return make(M)
	}
	if len(ms) == 1 {
		return cloneWithoutNilCheck(ms[0])
	}
	minLen := len(ms[0])
	for _, m := range ms[1:] {
		minLen = value.Min(minLen, len(m))
	}
	ret := make(M, minLen)
	// Fastpath: all maps are empty.
	if minLen == 0 {
		return ret
	}
	for k, v := range ms[0] {
		found := true // at least we found it in ms[0]
		for _, m := range ms[1:] {
			var tmp V
			if tmp, found = m[k]; !found {
				break
			} else {
				v = onConflict(k, v, tmp)
			}
		}
		if found {
			ret[k] = v
		}
	}
	return ret
}

// Load returns the value stored in the map for a key.
//
// If the value was not found in the map. of.Nil[V]() is returned.
//
// If the given map is nil, of.Nil[V]() is returned.
//
// 💡 HINT: See also [LoadAny], [LoadAll], [LoadSome] if you have multiple values
// to load.
//
// 💡 AKA: Get
func Load[K comparable, V any](m map[K]V, k K) of.O[V] {
	if m == nil || len(m) == 0 {
		return of.Nil[V]()
	}
	v, ok := m[k]
	if !ok {
		return of.Nil[V]()
	}
	return of.OK(v)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
//
// The loaded result is true if the value was loaded, false if stored.
//
// ⚠️ WARNING: LoadOrStore panics when a nil map is given.
//
// 💡 AKA: setdefault
func LoadOrStore[K comparable, V any](m map[K]V, k K, defaultV V) (v V, loaded bool) {
	assertNonNilMap(m)
	v, loaded = m[k]
	if !loaded {
		v = defaultV
		m[k] = v
	}
	return
}

// LoadOrStoreLazy returns the existing value for the key if present.
// Otherwise, it stores and returns the value that lazy computed by function f.
//
// The loaded result is true if the value was loaded, false if stored.
//
// ⚠️ WARNING: LoadOrStoreLazy panics when a nil map is given.
func LoadOrStoreLazy[K comparable, V any](m map[K]V, k K, f func() V) (v V, loaded bool) {
	assertNonNilMap(m)
	v, loaded = m[k]
	if !loaded {
		v = f()
		m[k] = v
	}
	return
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
//
// 🚀 EXAMPLE:
//
//	var m = map[string]int { "foo": 1 }
//	LoadAndDelete(m, "bar") ⏩ of.Nil()
//	LoadAndDelete(m, "foo") ⏩ of.OK(1)
//	LoadAndDelete(m, "foo") ⏩ of.Nil()
//
// 💡 HINT: If you want to delete an element "randomly", use [Pop].
func LoadAndDelete[K comparable, V any](m map[K]V, k K) of.O[V] {
	if m == nil || len(m) == 0 {
		return of.Nil[V]()
	}
	v, ok := m[k]
	if !ok {
		return of.Nil[V]()
	}
	delete(m, k)
	return of.OK(v)
}

// LoadKey find the first key that mapped to the specified value.
//
// 💡 NOTE: LoadKey has O(N) time complexity.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}
//	LoadKey(m, "1") ⏩ of.OK(1)
//	LoadKey(m, "a") ⏩ of.Nil[int]()
//
// 💡 AKA: FindKey, FindByKey, GetKey, GetByKey
func LoadKey[K, V comparable](m map[K]V, v V) of.O[K] {
	for k, vv := range m {
		if vv == v {
			return of.OK(k)
		}
	}
	return of.Nil[K]()
}

// LoadBy find the first value that satisfy the predicate f.
//
// 💡 NOTE: LoadBy has O(N) time complexity.
//
// 💡 AKA: FindBy, FindValueBy, GetBy, GetValueBy
func LoadBy[K comparable, V any](m map[K]V, f func(K, V) bool) of.O[V] {
	if len(m) == 0 {
		return of.Nil[V]()
	}
	for k, v := range m {
		if f(k, v) {
			return of.OK(v)
		}
	}
	return of.Nil[V]()
}

// LoadKeyBy find the first key that satisfy the predicate f.
//
// 💡 NOTE: LoadKeyBy has O(N) time complexity.
//
// 💡 AKA: FindKeyBy, GetKeyBy
func LoadKeyBy[K comparable, V any](m map[K]V, f func(K, V) bool) of.O[K] {
	if len(m) == 0 {
		return of.Nil[K]()
	}
	for k, v := range m {
		if f(k, v) {
			return of.OK(k)
		}
	}
	return of.Nil[K]()
}

// LoadAll returns the all value stored in the map for given keys.
//
// If not all keys are not found in the map, nil is returned.
// Otherwise, the length of returned values should equal the length of given keys.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "1", 2: "2", 3: "3"}
//	LoadAll(m, 1, 2) ⏩ []string{"1", "2"}
//	LoadAll(m, 1, 4) ⏩ []
func LoadAll[K comparable, V any](m map[K]V, ks ...K) []V {
	if m == nil || len(m) == 0 || len(ks) == 0 {
		return nil
	}
	vs := make([]V, 0, len(ks))
	for _, k := range ks {
		v, ok := m[k]
		if !ok {
			return nil
		}
		vs = append(vs, v)
	}
	return vs
}

// LoadAny returns the all value stored in the map for given keys.
//
// If no value is found in the map, of.Nil[V]() is returned.
// Otherwise, the first found value is returned.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "1", 2: "2", 3: "3"}
//	LoadAny(m, 1, 2) ⏩ of.OK("1")
//	LoadAny(m, 5, 1) ⏩ of.OK("1")
//	LoadAny(m, 5, 6) ⏩ of.Nil[string]()
func LoadAny[K comparable, V any](m map[K]V, ks ...K) (r of.O[V]) {
	if m == nil || len(m) == 0 || len(ks) == 0 {
		return
	}
	for _, k := range ks {
		if v, ok := m[k]; ok {
			return of.OK(v)
		}
	}
	return
}

// LoadSome returns the some values stored in the map for given keys.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{1: "1", 2: "2", 3: "3"}
//	LoadSome(m, 1, 2) ⏩ []string{"1", "2"}
//	LoadSome(m, 1, 4) ⏩ []string{"1"}
func LoadSome[K comparable, V any](m map[K]V, ks ...K) []V {
	if m == nil || len(m) == 0 || len(ks) == 0 {
		return nil
	}
	var vs []V
	for _, k := range ks {
		if v, ok := m[k]; ok {
			vs = append(vs, v)
		}
	}
	return vs
}

func assertNonNilMap[K comparable, V any](m map[K]V) {
	if m == nil {
		panic("nil map is not allowed")
	}
}

// PtrOf returns pointers that point to equivalent values of map m.
// (map[K]V → map[K]*V).
//
// 🚀 EXAMPLE:
//
//	PtrOf(map[int]string{1: "1", 2: "2"}) ⏩ map[int]*string{1: (*string)("1"), 2: (*string)("2")}
//
// ⚠️ WARNING: The returned pointers do not point to values of the original
// map, user CAN NOT modify the value by modifying the pointer.
func PtrOf[K comparable, V any](m map[K]V) map[K]*V {
	return MapValues(m, ptr.Of[V])
}

// Indirect returns the values pointed to by the pointers.
// If the pointer is nil, filter it out of the returned map.
//
// 🚀 EXAMPLE:
//
//		v1, v2 := "1", "2"
//	 m := map[int]*string{ 1: &v1, 2: &v2, 3: nil}
//	 Indirect(m) ⏩ map[int]string{1: "1", 2: "2"}
//
// 💡 HINT: If you want to replace nil pointer with default value,
// use [IndirectOr].
func Indirect[K comparable, V any](m map[K]*V) map[K]V {
	ret := make(map[K]V, len(m)/2)
	for k, v := range m {
		if v == nil {
			continue
		}
		ret[k] = *v
	}
	return ret
}

// IndirectOr is variant of [Indirect].
// If the pointer is nil, returns the fallback value instead.
//
// 🚀 EXAMPLE:
//
//		v1, v2 := "1", "2"
//	 m := map[int]*string{ 1: &v1, 2: &v2, 3: nil}
//	 IndirectOr(m, "nil") ⏩ map[int]string{1: "1", 2: "2", 3: "nil"}
func IndirectOr[K comparable, V any](m map[K]*V, fallback V) map[K]V {
	ret := make(map[K]V, len(m))
	for k, v := range m {
		if v == nil {
			ret[k] = fallback
		} else {
			ret[k] = *v
		}
	}
	return ret
}

// TypeAssert converts values of map from type From to type To by type assertion.
//
// 🚀 EXAMPLE:
//
//	TypeAssert[int](map[int]any{1: 1, 2: 2})   ⏩ map[int]int{1: 1, 2: 2}
//	TypeAssert[any](map[int]int{1: 1, 2: 2})   ⏩ map[int]any{1: 1, 2: 2}
//	TypeAssert[int64](map[int]int{1: 1, 2: 2}) ⏩ ❌PANIC❌
//
// ⚠️ WARNING:
//
//   - This function may ❌PANIC❌.
//     See [github.com/apus-run/gala/pkg/lang/value.TypeAssert] for more details
func TypeAssert[To any, K comparable, From any](m map[K]From) map[K]To {
	return MapValues(m, value.TypeAssert[To, From])
}

// Len returns the length of map m.
//
// 💡 HINT: This function is designed for high-order function, because the builtin
// function can not be used as function pointer.
// For example, if you want to get the total length of a 2D slice:
//
//	var s []map[int]int
//	total1 := SumBy(s, len)          // ❌ERROR❌ len (built-in) must be called
//	total2 := SumBy(s, Len[int,int]) // OK
func Len[K comparable, V any](m map[K]V) int {
	return len(m)
}

// Compact removes all zero values from given map m, returns a new map.
//
// 🚀 EXAMPLE:
//
//	m := map[int]string{0: "", 1: "foo", 2: "", 3: "bar"}
//	Compact(m) ⏩ map[int]string{1: "foo", 3: "bar"}
//
// 💡 HINT: See [github.com/apus-run/gala/pkg/lang/value.Zero] for details of zero value.
func Compact[M ~map[K]V, K, V comparable](m M) M {
	return FilterValues(m, value.IsNotZero[V])
}

// FilterMap does [Filter] and [Map] at the same time, applies function f to
// each key and value of map m. f returns (K2, V2, bool):
//
//   - If true ,the returned key and value will added to the result map[K2]V2.
//   - If false, the returned key and value will be dropped.
//
// 🚀 EXAMPLE:
//
//	f := func(k, v int) (string, string, bool) { return strconv.Itoa(k), strconv.Itoa(v), k != 0 && v != 0 }
//	FilterMap(map[int]int{1: 1, 2: 0, 0: 3}, f) ⏩ map[string]string{"1": "1"}
func FilterMap[K1, K2 comparable, V1, V2 any](m map[K1]V1, f func(K1, V1) (K2, V2, bool)) map[K2]V2 {
	r := make(map[K2]V2, len(m)/2)
	for k, v := range m {
		if kk, vv, ok := f(k, v); ok {
			r[kk] = vv
		}
	}
	return r
}

// TryFilterMap is a variant of [FilterMap] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	f := func(k, v int) (string, string, error) {
//		ki, kerr := strconv.Atoi(k)
//		vi, verr := strconv.Atoi(v)
//		return ki, vi, errors.Join(kerr, verr)
//	}
//	TryFilterMap(map[string]string{"1": "1", "2": "2"}, f) ⏩ map[int]int{1: 1, 2: 2}
//	TryFilterMap(map[string]string{"1": "a", "2": "2"}, f) ⏩ map[int]int{2: 2})
func TryFilterMap[K1, K2 comparable, V1, V2 any](m map[K1]V1, f func(K1, V1) (K2, V2, error)) map[K2]V2 {
	r := make(map[K2]V2, len(m)/2)
	for k, v := range m {
		if kk, vv, err := f(k, v); err == nil {
			r[kk] = vv
		}
	}
	return r
}

// FilterMapKeys is a variant of [FilterMap].
//
// 🚀 EXAMPLE:
//
//	f := func(v int) (string, bool) { return strconv.Itoa(v), v != 0 }
//	FilterMapKeys(map[int]int{1: 1, 2: 0, 0: 3}, f) ⏩ map[string]int{"1": 1, "2": 0}
func FilterMapKeys[K1, K2 comparable, V any](m map[K1]V, f func(K1) (K2, bool)) map[K2]V {
	r := make(map[K2]V, len(m)/2)
	for k, v := range m {
		if kk, ok := f(k); ok {
			r[kk] = v
		}
	}
	return r
}

// TryFilterMapKeys is a variant of [FilterMapKeys] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	FilterMapKeys(map[string]string{"1": "1", "2": "2"}, strconv.Atoi) ⏩ map[int]string{1: "1", 2: "2"}
//	FilterMapKeys(map[string]string{"1": "1", "a": "2"}, strconv.Atoi) ⏩ map[int]string{1: "1"}
func TryFilterMapKeys[K1, K2 comparable, V any](m map[K1]V, f func(K1) (K2, error)) map[K2]V {
	r := make(map[K2]V, len(m)/2)
	for k, v := range m {
		if kk, err := f(k); err == nil {
			r[kk] = v
		}
	}
	return r
}

// FilterMapValues is a variant of [FilterMap].
//
// 🚀 EXAMPLE:
//
//	f := func(v int) (string, bool) { return strconv.Itoa(v), v != 0 }
//	FilterMapValues(map[int]int{1: 1, 2: 0, 0: 3}, f) ⏩ map[int]string{1: "1", 0: "3"}
func FilterMapValues[K comparable, V1, V2 any](m map[K]V1, f func(V1) (V2, bool)) map[K]V2 {
	r := make(map[K]V2, len(m)/2)
	for k, v := range m {
		if vv, ok := f(v); ok {
			r[k] = vv
		}
	}
	return r
}

// TryFilterMapValues is a variant of [FilterMapValues] that allows function f to fail (return error).
//
// 🚀 EXAMPLE:
//
//	FilterMapValues(map[string]string{"1": "1", "2": "2"}, strconv.Atoi) ⏩ map[string]int{"1": 1, "2": 2}
//	FilterMapValues(map[string]string{"1": "1", "2": "a"}, strconv.Atoi) ⏩ map[string]int{"1": 1}
func TryFilterMapValues[K comparable, V1, V2 any](m map[K]V1, f func(V1) (V2, error)) map[K]V2 {
	r := make(map[K]V2, len(m)/2)
	for k, v := range m {
		if vv, err := f(v); err == nil {
			r[k] = vv
		}
	}
	return r
}
