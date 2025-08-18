package sliceutils_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/pkg/lang/result"
	"github.com/apus-run/gala/pkg/lang/sliceutils"
)

type randomTestStruct struct {
	blah string
}

func testMap[T, U any](t *testing.T, d string, s []T, f func(T) U, expectedOut []U) {
	t.Run(d, func(t *testing.T) {
		out := sliceutils.Map(s, f)
		require.Equal(t, expectedOut, out)
	})
}

func TestMap(t *testing.T) {
	testMap(t,
		"simple func on string",
		[]string{"1", "2"},
		func(s string) string {
			return s + s
		},
		[]string{"11", "22"},
	)
	testMap(t,
		"func of int to string array",
		[]int{1, 2},
		func(val int) []string {
			out := make([]string, 0, val)
			for i := 0; i < val; i++ {
				out = append(out, strconv.Itoa(i))
			}
			return out
		},
		[][]string{{"0"}, {"0", "1"}},
	)
	testMap(t,
		"extract element from struct",
		[]randomTestStruct{{"1"}, {"2"}},
		func(s randomTestStruct) string {
			return s.blah
		},
		[]string{"1", "2"},
	)
}

func TestToMap(t *testing.T) {
	type Foo struct {
		ID   int
		Name string
	}
	mapper := func(f Foo) (int, string) { return f.ID, f.Name }
	assert.Equal(t, map[int]string{}, sliceutils.ToMap([]Foo{}, mapper))
	assert.Equal(t, map[int]string{}, sliceutils.ToMap(nil, mapper))
	assert.Equal(t,
		map[int]string{1: "one", 2: "two", 3: "three"},
		sliceutils.ToMap([]Foo{{1, "one"}, {2, "two"}, {3, "three"}}, mapper))
}

func TestTryMap(t *testing.T) {
	assert.Equal(t, sliceutils.TryMap(sliceutils.Of("1", "2", "3"), strconv.Atoi), result.OK(sliceutils.Of(1, 2, 3)))
	assert.Equal(t, sliceutils.TryMap(nil, strconv.Atoi), result.OK(([]int{})))
	assert.Equal(t, sliceutils.TryMap(sliceutils.Of("1", "2", "a"), strconv.Atoi).Err().Error(), "strconv.Atoi: parsing \"a\": invalid syntax")
}

func Test2DSliceClone(t *testing.T) {
	a := assert.New(t)

	var slice2D [][]byte
	a.Nil(sliceutils.CloneShallow2DSlice(slice2D))

	slice2D = make([][]byte, 0)
	a.NotNil(sliceutils.CloneShallow2DSlice(slice2D))
	a.Equal(sliceutils.CloneShallow2DSlice(slice2D), [][]byte{})

	slice1 := []byte{'a', 'b'}
	slice2 := []byte{'c', 'd'}
	slice2D = [][]byte{slice1, slice2}
	cloned := sliceutils.CloneShallow2DSlice(slice2D)
	a.Len(slice2D, 2)
	a.Equal(cloned[0], slice1)
	a.Equal(cloned[1], slice2)

	slice1[0] = 'f'
	a.NotEqual(cloned[0], slice1)
}

func TestReversed(t *testing.T) {
	in := []string{"foo", "bar", "baz"}
	out := sliceutils.Reversed(in)
	assert.Equal(t, []string{"baz", "bar", "foo"}, out)
	assert.Equal(t, []string{"foo", "bar", "baz"}, in)
}

func TestSelect(t *testing.T) {
	input := []string{"foo", "bar", "baz", "qux"}
	cases := []struct {
		indices  []int
		expected []string
		panics   bool
	}{
		{
			indices:  []int{1, 3},
			expected: []string{"bar", "qux"},
		},
		{
			indices:  []int{2, 0},
			expected: []string{"baz", "foo"},
		},
		{
			indices:  []int{},
			expected: nil,
		},
		{
			indices:  []int{0, 0, 1, 1, 2, 2, 3, 3},
			expected: []string{"foo", "foo", "bar", "bar", "baz", "baz", "qux", "qux"},
		},
		{
			indices: []int{0, -1},
			panics:  true,
		},
		{
			indices: []int{0, 4},
			panics:  true,
		},
	}

	for _, testCase := range cases {
		c := testCase
		t.Run(fmt.Sprintf("%v", c.indices), func(t *testing.T) {
			if c.panics {
				assert.Panics(t, func() {
					sliceutils.Select(input, c.indices...)
				})
			} else {
				result := sliceutils.Select(input, c.indices...)
				assert.Equal(t, c.expected, result)
			}
		})
	}
}

type testType string

func (t testType) String() string {
	return string(t)
}

func TestStringSlice(t *testing.T) {
	in := []testType{
		"these", "are", "test", "values",
	}

	s := sliceutils.StringSlice(in...)

	assert.Equal(t, []string{"are", "test", "these", "values"}, s)
}

func TestFromStringSlice(t *testing.T) {
	in := []string{
		"these", "are", "test", "values",
	}

	testTypes := sliceutils.FromStringSlice[testType](in...)

	assert.IsType(t, []testType{}, testTypes)

	assert.ElementsMatch(t, []testType{"these", "are", "test", "values"}, testTypes)
}

func TestUnique(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c", "d"}, sliceutils.Unique([]string{"a", "b", "c", "a", "d", "d"}))
	assert.Equal(t, []string{"a", "b", "c", "d"}, sliceutils.Unique([]string{"a", "b", "c", "d"}))
	assert.Equal(t, []string{"a", "b"}, sliceutils.Unique([]string{"a", "a", "b", "a", "b"}))
	assert.Equal(t, []string{}, sliceutils.Unique([]string{}))
}
