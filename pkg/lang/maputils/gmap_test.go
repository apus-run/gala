package maputils

import (
	"fmt"
	"sort"
	"strconv"
	"testing"

	"github.com/apus-run/gala/pkg/lang/optional/of"
	"github.com/apus-run/gala/pkg/lang/ptr"
	"github.com/apus-run/gala/pkg/lang/result"
	"github.com/apus-run/gala/pkg/lang/sliceutils"
	"github.com/apus-run/gala/pkg/lang/value"

	"github.com/stretchr/testify/assert"
)

func TestMerge(t *testing.T) {
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Concat(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, nil))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Concat[int, int](nil, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{}, Concat[int, int](nil, nil))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Concat(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Concat(map[int]int{1: 0, 2: 0}, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Concat(map[int]int{1: 1, 2: 1}, map[int]int{2: 2, 3: 3, 4: 4}))
}

func TestMap(t *testing.T) {
	assert.Equal(t,
		map[string]string{"1": "1", "2": "2"},
		Map(map[int]int{1: 1, 2: 2}, func(k, v int) (string, string) {
			return strconv.Itoa(k), strconv.Itoa(v)
		}))
	assert.Equal(t,
		map[string]string{},
		Map(map[int]int{}, func(k, v int) (string, string) {
			return strconv.Itoa(k), strconv.Itoa(v)
		}))
}

func TestValues(t *testing.T) {
	{
		keys := Values(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"})
		sort.Strings(keys)
		assert.Equal(t, []string{"1", "2", "3", "4"}, keys)
	}
	assert.Equal(t, []string{}, Values(map[int]string{}))
	assert.Equal(t, []string{}, Values[int, string](nil))
}

func TestMapKeys(t *testing.T) {
	assert.Equal(t,
		map[string]int{"1": 1, "2": 2},
		MapKeys(map[int]int{1: 1, 2: 2}, strconv.Itoa))
	assert.Equal(t,
		map[string]int{},
		MapKeys(map[int]int{}, strconv.Itoa))
}

func TestTryMapKeys(t *testing.T) {
	assert.Equal(t,
		result.OK(map[int]int{}),
		TryMapKeys(map[string]int{}, strconv.Atoi))
	assert.Equal(t,
		result.OK(map[int]int{1: 1, 2: 2}),
		TryMapKeys(map[string]int{"1": 1, "2": 2}, strconv.Atoi))
	assert.Equal(t,
		"strconv.Atoi: parsing \"a\": invalid syntax",
		TryMapKeys(map[string]int{"1": 1, "a": 2}, strconv.Atoi).Err().Error())
}

func TestMapValues(t *testing.T) {
	assert.Equal(t,
		map[int]string{1: "1", 2: "2"},
		MapValues(map[int]int{1: 1, 2: 2}, strconv.Itoa))
	assert.Equal(t,
		map[int]string{},
		MapValues(map[int]int{}, strconv.Itoa))
}

func TestTryMapValues(t *testing.T) {
	assert.Equal(t,
		result.OK(map[int]int{}),
		TryMapValues(map[int]string{}, strconv.Atoi))
	assert.Equal(t,
		result.OK(map[int]int{1: 1, 2: 2}),
		TryMapValues(map[int]string{1: "1", 2: "2"}, strconv.Atoi))
	assert.Equal(t,
		"strconv.Atoi: parsing \"a\": invalid syntax",
		TryMapValues(map[int]string{1: "1", 2: "a"}, strconv.Atoi).Err().Error())
}

func TestFilter(t *testing.T) {
	assert.Equal(t,
		map[int]int{1: 1, 2: 2},
		Filter(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k, v int) bool { return (k+v)%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		Filter(map[int]int{}, func(k, v int) bool { return (k+v)%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		Filter(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k, v int) bool { return k+v > 100 }))
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 2, 4: 3},
		Filter(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k, v int) bool { return k+v > 0 }))
}

func TestFilterKeys(t *testing.T) {
	assert.Equal(t,
		map[int]int{2: 2, 4: 3},
		FilterKeys(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k int) bool { return k%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		FilterKeys(map[int]int{}, func(k int) bool { return k%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		FilterKeys(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k int) bool { return k > 100 }))
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 2, 4: 3},
		FilterKeys(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k int) bool { return k > 0 }))
}

func TestFilterByKeys(t *testing.T) {
	tests := []struct {
		name   string
		input  map[int]string
		keys   []int
		expect map[int]string
	}{
		{
			name:   "basic filtering",
			input:  map[int]string{1: "a", 2: "b", 3: "c", 4: "d"},
			keys:   []int{1, 3},
			expect: map[int]string{1: "a", 3: "c"},
		},
		{
			name:   "empty keys",
			input:  map[int]string{1: "a", 2: "b"},
			keys:   []int{},
			expect: map[int]string{},
		},
		{
			name:   "non-existent keys",
			input:  map[int]string{1: "a", 2: "b"},
			keys:   []int{3, 4},
			expect: map[int]string{},
		},
		{
			name:   "partially existing keys",
			input:  map[int]string{1: "a", 2: "b", 3: "c"},
			keys:   []int{1, 3, 5},
			expect: map[int]string{1: "a", 3: "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByKeys(tt.input, tt.keys...)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestFilterValues(t *testing.T) {
	assert.Equal(t,
		map[int]int{2: 2, 3: 2},
		FilterValues(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(v int) bool { return v%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		FilterValues(map[int]int{}, func(v int) bool { return v%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		FilterValues(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(v int) bool { return v > 100 }))
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 2, 4: 3},
		FilterValues(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(v int) bool { return v > 0 }))
}

func TestFilterByValues(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]int
		values []int
		expect map[string]int
	}{
		{
			name:   "basic filtering",
			input:  map[string]int{"a": 1, "b": 2, "c": 1, "d": 3},
			values: []int{1, 3},
			expect: map[string]int{"a": 1, "c": 1, "d": 3},
		},
		{
			name:   "empty values",
			input:  map[string]int{"a": 1, "b": 2},
			values: []int{},
			expect: map[string]int{},
		},
		{
			name:   "non-existent values",
			input:  map[string]int{"a": 1, "b": 2},
			values: []int{3, 4},
			expect: map[string]int{},
		},
		{
			name:   "duplicate values",
			input:  map[string]int{"a": 1, "b": 2, "c": 1, "d": 1},
			values: []int{1},
			expect: map[string]int{"a": 1, "c": 1, "d": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByValues(tt.input, tt.values...)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestReject(t *testing.T) {
	assert.Equal(t,
		map[int]int{1: 1, 2: 2},
		Reject(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k, v int) bool { return (k+v)%2 != 0 }))
	assert.Equal(t,
		map[int]int{},
		Reject(map[int]int{}, func(k, v int) bool { return (k+v)%2 != 0 }))
	assert.Equal(t,
		map[int]int{},
		Reject(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k, v int) bool { return k+v < 100 }))
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 2, 4: 3},
		Reject(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k, v int) bool { return k+v < 0 }))
}

func TestRejectKeys(t *testing.T) {
	assert.Equal(t,
		map[int]int{2: 2, 4: 3},
		RejectKeys(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k int) bool { return k%2 != 0 }))
	assert.Equal(t,
		map[int]int{},
		RejectKeys(map[int]int{}, func(k int) bool { return k%2 != 0 }))
	assert.Equal(t,
		map[int]int{},
		RejectKeys(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k int) bool { return k < 100 }))
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 2, 4: 3},
		RejectKeys(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(k int) bool { return k < 0 }))
}

func TestRejectByKeys(t *testing.T) {
	tests := []struct {
		name   string
		input  map[int]string
		keys   []int
		expect map[int]string
	}{
		{
			name:   "basic rejection",
			input:  map[int]string{1: "a", 2: "b", 3: "c", 4: "d"},
			keys:   []int{1, 3},
			expect: map[int]string{2: "b", 4: "d"},
		},
		{
			name:   "empty keys",
			input:  map[int]string{1: "a", 2: "b"},
			keys:   []int{},
			expect: map[int]string{1: "a", 2: "b"},
		},
		{
			name:   "non-existent keys",
			input:  map[int]string{1: "a", 2: "b"},
			keys:   []int{3, 4},
			expect: map[int]string{1: "a", 2: "b"},
		},
		{
			name:   "partially existing keys",
			input:  map[int]string{1: "a", 2: "b", 3: "c"},
			keys:   []int{1, 3, 5},
			expect: map[int]string{2: "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RejectByKeys(tt.input, tt.keys...)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestRejectValues(t *testing.T) {
	assert.Equal(t,
		map[int]int{2: 2, 3: 2},
		RejectValues(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(v int) bool { return v%2 != 0 }))
	assert.Equal(t,
		map[int]int{},
		RejectValues(map[int]int{}, func(v int) bool { return v%2 == 0 }))
	assert.Equal(t,
		map[int]int{},
		RejectValues(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(v int) bool { return v < 100 }))
	assert.Equal(t,
		map[int]int{1: 1, 2: 2, 3: 2, 4: 3},
		RejectValues(map[int]int{1: 1, 2: 2, 3: 2, 4: 3}, func(v int) bool { return v < 0 }))
}

func TestRejectByValues(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]int
		values []int
		expect map[string]int
	}{
		{
			name:   "basic rejection",
			input:  map[string]int{"a": 1, "b": 2, "c": 1, "d": 3},
			values: []int{1, 3},
			expect: map[string]int{"b": 2},
		},
		{
			name:   "empty values",
			input:  map[string]int{"a": 1, "b": 2},
			values: []int{},
			expect: map[string]int{"a": 1, "b": 2},
		},
		{
			name:   "non-existent values",
			input:  map[string]int{"a": 1, "b": 2},
			values: []int{3, 4},
			expect: map[string]int{"a": 1, "b": 2},
		},
		{
			name:   "duplicate values in input",
			input:  map[string]int{"a": 1, "b": 2, "c": 1, "d": 1},
			values: []int{1},
			expect: map[string]int{"b": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RejectByValues(tt.input, tt.values...)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestFold(t *testing.T) {
	assert.Equal(t,
		6,
		Fold(map[int]int{1: 1, 2: 2}, func(acc, k, v int) int { return acc + k + v }, 0))
	assert.Equal(t,
		9,
		Fold(map[int]int{1: 1, 2: 2}, func(acc, k, v int) int { return acc + k + v }, 3))
	assert.Equal(t,
		0,
		Fold(map[int]int{}, func(acc, k, v int) int { return acc + k + v }, 0))
	assert.Equal(t,
		3,
		Fold(map[int]int{}, func(acc, k, v int) int { return acc + k + v }, 3))
}

func TestFoldKeys(t *testing.T) {
	assert.Equal(t,
		3,
		FoldKeys(map[int]int{1: 2, 2: 4}, value.Add[int], 0))
	assert.Equal(t,
		5,
		FoldKeys(map[int]int{1: 2, 2: 4}, value.Add[int], 2))
	assert.Equal(t,
		0,
		FoldKeys(map[int]int{}, value.Add[int], 0))
	assert.Equal(t,
		2,
		FoldKeys(map[int]int{}, value.Add[int], 2))
}

func TestFoldValues(t *testing.T) {
	assert.Equal(t,
		6,
		FoldValues(map[int]int{1: 2, 2: 4}, value.Add[int], 0))
	assert.Equal(t,
		8,
		FoldValues(map[int]int{1: 2, 2: 4}, value.Add[int], 2))
	assert.Equal(t,
		0,
		FoldValues(map[int]int{}, value.Add[int], 0))
	assert.Equal(t,
		2,
		FoldValues(map[int]int{}, value.Add[int], 2))
}

func TestLoad(t *testing.T) {
	assert.Equal(t, of.Nil[int](), Load[int, int](nil, 1))
	assert.Equal(t, of.OK(1),
		Load(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, 1))
	assert.Equal(t, of.OK(2),
		Load(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, 2))
	assert.Equal(t, of.Nil[int](),
		Load(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, 5))
}

func TestLoadAndDelete(t *testing.T) {
	{
		assert.Equal(t, of.Nil[int](), LoadAndDelete[int, int](nil, 1))
	}
	{
		m := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}
		assert.Equal(t, of.OK(1), LoadAndDelete(m, 1))
		assert.Equal(t, map[int]int{2: 2, 3: 3, 4: 4}, m)
	}
	{
		m := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}
		assert.Equal(t, of.OK(2), LoadAndDelete(m, 2))
		assert.Equal(t, map[int]int{1: 1, 3: 3, 4: 4}, m)
	}
	{
		m := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}
		assert.Equal(t, of.Nil[int](), LoadAndDelete(m, 5))
		assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, m)
	}
}

func TestEqual(t *testing.T) {
	assert.True(t, Equal(
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.False(t, Equal(
		map[int]int{1: 1, 2: 2, 3: 3},
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.False(t, Equal(
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]int{1: 1, 2: 2, 3: 3}))
	assert.False(t, Equal(
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]int{1: 1, 2: 2, 3: 3, 4: 5}))
	assert.False(t, Equal(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, nil))
	assert.False(t, Equal(nil, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.True(t, Equal(map[int]int{}, map[int]int{}))
	assert.True(t, Equal(nil, map[int]int{}))
	assert.True(t, Equal(map[int]int{}, nil))
	assert.True(t, Equal[int, int](nil, nil))
}

func TestEqualBy(t *testing.T) {
	eq := value.Equal[int]
	assert.True(t, EqualBy(
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, eq))
	assert.False(t, EqualBy(
		map[int]int{1: 1, 2: 2, 3: 3},
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, eq))
	assert.False(t, EqualBy(
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]int{1: 1, 2: 2, 3: 3}, eq))
	assert.False(t, EqualBy(
		map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]int{1: 1, 2: 2, 3: 3, 4: 5}, eq))
	assert.False(t, EqualBy(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, nil, eq))
	assert.False(t, EqualBy(nil, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, eq))
	assert.True(t, EqualBy(map[int]int{}, map[int]int{}, eq))
	assert.True(t, EqualBy(nil, map[int]int{}, eq))
	assert.True(t, EqualBy(map[int]int{}, nil, eq))
	assert.True(t, EqualBy[int](nil, nil, eq))

	anyEq := func(v1, v2 any) bool { return v1 == v2 }
	assert.True(t, EqualBy(
		map[int]any{1: 1, 2: 2, 3: 3, 4: 4},
		map[int]any{1: 1, 2: 2, 3: 3, 4: 4}, anyEq))
}

func TestClone(t *testing.T) {
	assert.Equal(t, map[int]int{1: 1, 2: 2}, Clone(map[int]int{1: 1, 2: 2}))
	var nilMap map[int]int
	assert.Equal(t, map[int]int{}, Clone(map[int]int{}))
	assert.NotEqual(t, nil, Clone(map[int]int{}))
	assert.Equal(t, nil, Clone(nilMap))
	assert.NotEqual(t, map[int]int{}, Clone(nilMap))

	// Test new type.
	type I2I map[int]int
	assert.Equal(t, I2I{1: 1, 2: 2}, Clone(I2I{1: 1, 2: 2}))
	assert.Equal(t, "gmap.I2I", fmt.Sprintf("%T", Clone(I2I{})))

	// Test shallow clone.
	src := map[int]*int{1: ptr.Of(1), 2: ptr.Of(2)}
	dst := Clone(src)
	assert.Equal(t, src, dst)
	assert.True(t, src[1] == dst[1])
	assert.True(t, src[2] == dst[2])
}

func TestInvert(t *testing.T) {
	assert.Equal(t, map[int]string{}, Invert(map[string]int{}))
	assert.Equal(t, map[int]string{1: "1", 2: "2"}, Invert(map[string]int{"1": 1, "2": 2}))

	// Test custom type.
	type X struct{ Foo int }
	type Y struct{ Bar int }

	assert.Equal(t, map[Y]X{{Bar: 2}: {Foo: 1}}, Invert(map[X]Y{{Foo: 1}: {Bar: 2}}))
}

func TestLoadKey(t *testing.T) {
	assert.Equal(t, of.Nil[int](), LoadKey[int, string](nil, ""))
	assert.Equal(t, of.OK(1),
		LoadKey(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, "1"))
	assert.Equal(t, of.OK(2),
		LoadKey(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, "2"))
	assert.Equal(t, of.Nil[int](),
		LoadKey(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, "5"))
}

func TestLoadBy(t *testing.T) {
	assert.Equal(t, of.Nil[string](),
		LoadBy[int, string](nil, func(k int, v string) bool {
			return v == ""
		}))
	assert.Equal(t, of.OK("1"),
		LoadBy(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, func(k int, v string) bool {
			return k == 1
		}))
	assert.Equal(t, of.OK("2"),
		LoadBy(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, func(k int, v string) bool {
			return v == "2"
		}))
	assert.Equal(t, of.Nil[string](),
		LoadBy(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, func(k int, v string) bool {
			return k == 0 || v == ""
		}))
}

func TestLoadKeyBy(t *testing.T) {
	assert.Equal(t, of.Nil[int](),
		LoadKeyBy[int, string](nil, func(k int, v string) bool {
			return v == ""
		}))
	assert.Equal(t, of.OK(1),
		LoadKeyBy(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, func(k int, v string) bool {
			return k == 1
		}))
	assert.Equal(t, of.OK(2),
		LoadKeyBy(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, func(k int, v string) bool {
			return v == "2"
		}))
	assert.Equal(t, of.Nil[int](),
		LoadKeyBy(map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}, func(k int, v string) bool {
			return k == 0 || v == ""
		}))
}

func TestContains(t *testing.T) {
	assert.False(t, Contains(map[int]string{}, 1))
	assert.False(t, Contains[int, string](nil, 1))
	assert.True(t, Contains(map[int]string{1: "", 2: ""}, 1))
	assert.False(t, Contains(map[int]string{1: "", 2: ""}, 3))
}

func TestContainsAny(t *testing.T) {
	assert.False(t, ContainsAny(map[int]string{}, 1))
	assert.False(t, ContainsAny[int, string](nil, 1))
	assert.False(t, ContainsAny[int, string](nil))
	assert.True(t, ContainsAny(map[int]string{1: "", 2: ""}, 1, 2))
	assert.True(t, ContainsAny(map[int]string{1: "", 2: ""}, 1, 3))
	assert.False(t, ContainsAny(map[int]string{1: "", 2: ""}, 3, 4))
}

func TestContainsAll(t *testing.T) {
	assert.False(t, ContainsAll(map[int]string{}, 1))
	assert.False(t, ContainsAll[int, string](nil, 1))
	assert.True(t, ContainsAll[int, string](nil))
	assert.True(t, ContainsAll(map[int]string{1: "", 2: ""}, 1, 2))
	assert.False(t, ContainsAll(map[int]string{1: "", 2: ""}, 1, 3))
}

func TestLoadAll(t *testing.T) {
	assert.Equal(t, nil, LoadAll(map[int]int{}, 1, 2, 3))
	assert.Equal(t, nil, LoadAll(map[int]string{1: "1", 2: "2"}))
	assert.Equal(t, []string{"1", "2"},
		LoadAll(map[int]string{1: "1", 2: "2", 3: "3"}, 1, 2))
	assert.Equal(t, nil,
		LoadAll(map[int]string{1: "1", 2: "2", 3: "3"}, 1, 4))
}

func TestLoadAny(t *testing.T) {
	assert.Equal(t, of.Nil[int](), LoadAny(map[int]int{}, 1, 2, 3))
	assert.Equal(t, of.Nil[string](), LoadAny(map[int]string{1: "1", 2: "2"}))
	assert.Equal(t, of.OK("1"),
		LoadAny(map[int]string{1: "1", 2: "2", 3: "3"}, 1, 2))
	assert.Equal(t, of.OK("2"),
		LoadAny(map[int]string{1: "1", 2: "2", 3: "3"}, 2, 1))
	assert.Equal(t, of.OK("1"),
		LoadAny(map[int]string{1: "1", 2: "2", 3: "3"}, 9, 1))
	assert.Equal(t, of.Nil[string](),
		LoadAny(map[int]string{1: "1", 2: "2", 3: "3"}, 9, 10))
}

func TestLoadSome(t *testing.T) {
	assert.Equal(t, nil, LoadSome(map[int]int{}, 1, 2, 3))
	assert.Equal(t, nil, LoadSome(map[int]string{1: "1", 2: "2"}))
	assert.Equal(t, []string{"1", "2"},
		LoadSome(map[int]string{1: "1", 2: "2", 3: "3"}, 1, 2))
	assert.Equal(t, []string{"1"},
		LoadSome(map[int]string{1: "1", 2: "2", 3: "3"}, 1, 4))
}

func TestPtrOf(t *testing.T) {
	{
		m := map[int]string{1: "1", 2: "2"}
		ptrs := PtrOf(m)
		assert.Equal(t, map[int]*string{1: ptr.Of("1"), 2: ptr.Of("2")}, ptrs)
	}

	// Test modifying pointer.
	{
		m := map[int]string{1: "1", 2: "2"}
		ptrs := PtrOf(m)
		*ptrs[1] = ""
		assert.Equal(t, "", *ptrs[1])
		assert.Equal(t, "1", m[1])
	}
}

func TestIndirect(t *testing.T) {
	assert.Equal(t,
		map[int]string{1: "1", 2: "2"},
		Indirect(map[int]*string{1: ptr.Of("1"), 2: ptr.Of("2"), 3: nil}))
	assert.Equal(t,
		map[int]string{1: "1", 2: "2"},
		Indirect(map[int]*string{1: ptr.Of("1"), 2: ptr.Of("2")}))
}

func TestIndirectOr(t *testing.T) {
	assert.Equal(t,
		map[int]string{1: "1", 2: "2", 3: ""},
		IndirectOr(map[int]*string{1: ptr.Of("1"), 2: ptr.Of("2"), 3: nil}, ""))
	assert.Equal(t,
		map[int]string{1: "1", 2: "2"},
		IndirectOr(map[int]*string{1: ptr.Of("1"), 2: ptr.Of("2")}, ""))
}

func TestLen(t *testing.T) {
	assert.Equal(t, 0, Len(map[int]int{}))
	assert.Equal(t, 2, Len(map[int]int{1: 1, 2: 2}))
}

func TestUnion(t *testing.T) {
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Union(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, nil))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Union(nil, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))

	// Empty
	assert.Equal(t, map[int]string{}, Union[map[int]string]())
	assert.Equal(t, map[int]int{}, Union(map[int]int(nil)))
	assert.Equal(t, map[int]int{}, Union(map[int]int(nil), nil))
	assert.Equal(t, map[int]int{}, Union(map[int]int(nil), nil, nil))

	// New value replace old.
	assert.Equal(t, map[int]int{1: 3},
		Union(
			map[int]int{1: 1},
			map[int]int{1: 2},
			map[int]int{},
			map[int]int{1: 3}))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Union(
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Union(
			map[int]int{1: 0, 2: 0},
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Union(
			map[int]int{1: 1, 2: 1},
			map[int]int{2: 2, 3: 3, 4: 4}))
}

func TestUnionBy(t *testing.T) {
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		UnionBy(sliceutils.Of(map[int]int{1: 1, 2: 2, 3: 3, 4: 4}, nil), nil))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Union(nil, map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))

	// Empty
	assert.Equal(t, map[int]string{}, UnionBy[map[int]string](nil, nil))
	assert.Equal(t, map[int]int{}, UnionBy([]map[int]int{nil}, nil))
	assert.Equal(t, map[int]int{}, UnionBy([]map[int]int{nil, nil}, nil))
	assert.Equal(t, map[int]int{}, UnionBy([]map[int]int{nil, nil, nil}, nil))

	// Nil [ConflictFunc] causes PANIC.
	assert.Panics(t, func() {
		_ = UnionBy(
			sliceutils.Of(
				map[int]int{1: 1},
				map[int]int{1: 2},
				map[int]int{},
				map[int]int{1: 3}),
			nil)
	})
}

func TestDiff(t *testing.T) {
	assert.Equal(t, map[int]string{}, Diff(map[int]string{}))
	assert.Equal(t, map[int]string{1: "1"}, Diff(map[int]string{1: "1"}))
	assert.Equal(t, map[int]string{1: "1", 2: "2"}, Diff(map[int]string{1: "1", 2: "2"}))
	assert.Equal(t, map[int]string{1: "1"}, Diff(map[int]string{1: "1"}, nil))
	assert.Equal(t, map[int]string{1: "1"}, Diff(map[int]string{1: "1"}, nil, nil, nil))

	assert.Equal(t, map[int]int{2: 2, 3: 3},
		Diff(
			map[int]int{1: 1, 2: 2, 3: 3},
			map[int]int{1: 2}, map[int]int{1: 3}))
	assert.Equal(t, map[int]int{},
		Diff(
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{},
		Diff(
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
			map[int]int{1: 1, 2: 2}, map[int]int{3: 3, 4: 4}))
	assert.Equal(t, map[int]int{1: 1, 5: 5},
		Diff(
			map[int]int{1: 1, 2: 1, 5: 5},
			map[int]int{2: 2, 3: 3, 4: 4}))
}

func TestIntersect(t *testing.T) {
	assert.Equal(t, map[int]int{}, Intersect[map[int]int]())
	assert.Equal(t, map[int]int{}, Intersect(map[int]int(nil)))
	assert.Equal(t, map[int]int{}, Intersect(map[int]int(nil), nil))
	assert.Equal(t, map[int]int{}, Intersect(map[int]int(nil), nil, nil))

	assert.Equal(t, map[int]int{}, Intersect(nil, map[int]int{1: 1}, nil))
	assert.Equal(t, map[int]int{}, Intersect(map[int]int{1: 1}, nil, nil))
	assert.Equal(t, map[int]int{}, Intersect(nil, nil, map[int]int{1: 1}, nil))

	assert.Equal(t, map[int]int{1: 1, 2: 2},
		Intersect(map[int]int{1: 1, 2: 2}, map[int]int{1: 1, 2: 2, 3: 3}))
	assert.Equal(t, map[int]int{1: 1, 2: 2},
		Intersect(map[int]int{1: 1, 2: 2, 3: 3}, map[int]int{1: 1, 2: 2}))

	// New value replaces old one.
	assert.Equal(t, map[int]int{1: 1, 2: -1},
		Intersect(map[int]int{1: 1, 2: 2}, map[int]int{1: 1, 2: -1, 3: 3}))
	assert.Equal(t, map[int]int{1: 1, 2: -1},
		Intersect(map[int]int{1: 1, 2: 2, 3: 3}, map[int]int{1: 1, 2: -1}))

	assert.Equal(t, map[int]int{1: 3},
		Intersect(
			map[int]int{1: 1, 2: 2, 3: 3},
			map[int]int{1: 2},
			map[int]int{1: 3}))
	assert.Equal(t, map[int]string{1: "1"}, Intersect(map[int]string{1: "1"}))

	assert.Equal(t, map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
		Intersect(
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4}))
	assert.Equal(t, map[int]int{},
		Intersect(
			map[int]int{1: 1, 2: 2, 3: 3, 4: 4},
			map[int]int{1: 1, 2: 2},
			map[int]int{3: 3, 4: 4}))
	assert.Equal(t, map[int]int{1: 1, 2: 2, 5: 5},
		Intersect(
			map[int]int{1: 1, 2: 1, 5: 5},
			map[int]int{2: 2, 3: 3, 4: 4, 1: 1, 5: 5}))
}

func TestIntersectBy(t *testing.T) {
	// Empty
	assert.Equal(t, map[int]int{}, IntersectBy[map[int]int](nil, nil))
	assert.Equal(t, map[int]int{}, IntersectBy([]map[int]int{}, nil))
	assert.Equal(t, map[int]int{}, IntersectBy([]map[int]int{nil}, nil))
	assert.Equal(t, map[int]int{}, IntersectBy([]map[int]int{nil, nil}, nil))
	assert.Equal(t, map[int]int{}, IntersectBy([]map[int]int{nil, nil, nil}, nil))

	assert.Equal(t, map[int]int{}, IntersectBy(sliceutils.Of(nil, map[int]int{1: 1}, nil), nil))
	assert.Equal(t, map[int]int{}, IntersectBy(sliceutils.Of(map[int]int{1: 1}, nil, nil), nil))
	assert.Equal(t, map[int]int{}, IntersectBy(sliceutils.Of(nil, nil, map[int]int{1: 1}, nil), nil))

	// Nil [ConflictFunc] causes PANIC.
	assert.Panics(t, func() {
		_ = IntersectBy(
			sliceutils.Of(
				map[int]int{1: 1},
				map[int]int{1: 2},
				map[int]int{1: 3}),
			nil)
	})
}

func TestCompact(t *testing.T) {
	assert.Equal(t, map[int]int{}, Compact(map[int]int(nil)))
	assert.Equal(t, map[int]int{}, Compact(map[int]int{}))
	assert.Equal(t,
		map[int]string{1: "foo", 3: "bar"},
		Compact(map[int]string{0: "", 1: "foo", 2: "", 3: "bar"}))
	assert.Equal(t,
		map[int]string{0: "foo", 1: "foo", 2: "bar", 3: "bar"},
		Compact(map[int]string{0: "foo", 1: "foo", 2: "bar", 3: "bar"}))
	assert.Equal(t,
		map[int]string{},
		Compact(map[int]string{0: "", 1: "", 2: "", 3: ""}))
}

func TestFilterMapKeys(t *testing.T) {
	parseInt := func(s string) (int, bool) {
		ki, err := strconv.ParseInt(s, 10, 64)
		return int(ki), err == nil
	}
	assert.Equal(t,
		map[int]string{1: "1", 2: "2", 4: "b"},
		FilterMapKeys(map[string]string{"1": "1", "2": "2", "a": "3", "4": "b", "c": "c"}, parseInt))
	assert.Equal(t,
		map[int]string{4: "b"},
		FilterMapKeys(map[string]string{"a": "3", "4": "b"}, parseInt))
	assert.Equal(t,
		map[int]string{1: "1", 2: "2"},
		FilterMapKeys(map[string]string{"1": "1", "2": "2"}, parseInt))
	assert.Equal(t,
		map[int]string{},
		FilterMapKeys(map[string]string{}, parseInt))
	assert.Equal(t,
		map[int]string{},
		FilterMapKeys((map[string]string)(nil), parseInt))
}

func TestTryFilterMapKeys(t *testing.T) {
	parseInt := strconv.Atoi
	assert.Equal(t,
		map[int]string{1: "1", 2: "2", 4: "b"},
		TryFilterMapKeys(map[string]string{"1": "1", "2": "2", "a": "3", "4": "b", "c": "c"}, parseInt))
	assert.Equal(t,
		map[int]string{4: "b"},
		TryFilterMapKeys(map[string]string{"a": "3", "4": "b"}, parseInt))
	assert.Equal(t,
		map[int]string{1: "1", 2: "2"},
		TryFilterMapKeys(map[string]string{"1": "1", "2": "2"}, parseInt))
	assert.Equal(t,
		map[int]string{},
		TryFilterMapKeys(map[string]string{}, parseInt))
	assert.Equal(t,
		map[int]string{},
		TryFilterMapKeys((map[string]string)(nil), parseInt))
}

func TestFilterMapValues(t *testing.T) {
	parseInt := func(s string) (int, bool) {
		ki, err := strconv.ParseInt(s, 10, 64)
		return int(ki), err == nil
	}
	assert.Equal(t,
		map[string]int{"1": 1, "2": 2, "a": 3},
		FilterMapValues(map[string]string{"1": "1", "2": "2", "a": "3", "4": "b", "c": "c"}, parseInt))
	assert.Equal(t,
		map[string]int{"a": 3},
		FilterMapValues(map[string]string{"a": "3", "4": "b"}, parseInt))
	assert.Equal(t,
		map[string]int{"1": 1, "2": 2},
		FilterMapValues(map[string]string{"1": "1", "2": "2"}, parseInt))
	assert.Equal(t,
		map[string]int{},
		FilterMapValues(map[string]string{}, parseInt))
	assert.Equal(t,
		map[string]int{},
		FilterMapValues((map[string]string)(nil), parseInt))
}

func TestTryFilterMapValues(t *testing.T) {
	parseInt := strconv.Atoi
	assert.Equal(t,
		map[string]int{"1": 1, "2": 2, "a": 3},
		TryFilterMapValues(map[string]string{"1": "1", "2": "2", "a": "3", "4": "b", "c": "c"}, parseInt))
	assert.Equal(t,
		map[string]int{"a": 3},
		TryFilterMapValues(map[string]string{"a": "3", "4": "b"}, parseInt))
	assert.Equal(t,
		map[string]int{"1": 1, "2": 2},
		TryFilterMapValues(map[string]string{"1": "1", "2": "2"}, parseInt))
	assert.Equal(t,
		map[string]int{},
		TryFilterMapValues(map[string]string{}, parseInt))
	assert.Equal(t,
		map[string]int{},
		TryFilterMapValues((map[string]string)(nil), parseInt))
}

func TestCount(t *testing.T) {
	assert.Equal(t, 0, Count(map[int]string{}, "2"))
	assert.Equal(t, 1, Count(map[int]string{1: "1", 2: "2", 3: "3"}, "2"))
	assert.Equal(t, 2, Count(map[int]string{1: "1", 2: "2", 3: "2"}, "2"))
	assert.Equal(t, 3, Count(map[int]string{1: "2", 2: "2", 3: "2"}, "2"))
	assert.Equal(t, 1, Count(map[int]string{1: "2", 2: "2", 3: "3"}, "3"))
	assert.Equal(t, 0, Count(map[int]string{1: "2", 2: "2", 3: "4"}, "3"))
}

func TestCountBy(t *testing.T) {
	f := func(k int, v string) bool {
		i, _ := strconv.Atoi(v)
		return k%2 == 1 && i%2 == 1
	}
	assert.Equal(t, 0, CountBy(map[int]string{}, f))
	assert.Equal(t, 2, CountBy(map[int]string{1: "1", 2: "2", 3: "3"}, f))
	assert.Equal(t, 1, CountBy(map[int]string{1: "1", 2: "2", 3: "2"}, f))
	assert.Equal(t, 1, CountBy(map[int]string{1: "1", 2: "2", 4: "3"}, f))
}

func TestCountValueBy(t *testing.T) {
	f := func(v string) bool {
		i, _ := strconv.Atoi(v)
		return i%2 == 1
	}
	assert.Equal(t, 0, CountValueBy(map[int]string{}, f))
	assert.Equal(t, 2, CountValueBy(map[int]string{1: "1", 2: "2", 3: "3"}, f))
	assert.Equal(t, 1, CountValueBy(map[int]string{1: "1", 2: "2", 3: "2"}, f))
	assert.Equal(t, 2, CountValueBy(map[int]string{1: "1", 2: "2", 4: "3"}, f))
}
