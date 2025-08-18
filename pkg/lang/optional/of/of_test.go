package of_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/pkg/lang/optional/of" // Assuming utils package contains necessary utility functions
	"github.com/apus-run/gala/pkg/lang/ptr"
)

/*
// UsersQuery GetUsers
type UsersQuery struct {
	Name                        of.O[string]
	IsBot                       of.O[bool]
	IsActive                    of.O[bool]
	IsCMemberOf                 of.O[uuid.UUID]
	EnableProfileLoading        bool
}

// NotBot
func (q UsersQuery) NotBot() UsersQuery {
	q.IsBot = of.From(false)
	return q
}

// NameOf
func (q UsersQuery) NameOf(name string) UsersQuery {
	q.Name = of.From(name)
	return q
}

// Active
func (q UsersQuery) Active() UsersQuery {
	q.IsActive = of.From(true)
	return q
}

// CMemberOf
func (q UsersQuery) CMemberOf(channelID uuid.UUID) UsersQuery {
	q.IsCMemberOf = of.From(channelID)
	return q
}

// LoadProfile
func (q UsersQuery) LoadProfile() UsersQuery {
	q.EnableProfileLoading = true
	return q
}
*/

func foo1() (int, bool) {
	return 1, true
}

func foo2() of.O[int] {
	return of.OK(1)
}

func Benchmark(b *testing.B) {
	b.Run("(int,bool)", func(b *testing.B) {
		for i := 0; i <= b.N; i++ {
			v, ok := foo1()
			if !ok || v != 1 {
				b.FailNow()
			}
		}
	})
	b.Run("of", func(b *testing.B) {
		for i := 0; i <= b.N; i++ {
			o := foo2()
			if !o.IsOK() || o.Val() != 1 {
				b.FailNow()
			}
		}
	})
}

func TestOf_ValueOrZero(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		var o of.O[int]
		assert.EqualValues(t, 0, o.ValueOrZero())
	})
	t.Run("invalid, has value", func(t *testing.T) {
		o := of.O[int]{Valid: false, V: 123}
		assert.EqualValues(t, 0, o.ValueOrZero())
	})
	t.Run("valid", func(t *testing.T) {
		o := of.From(123)
		assert.EqualValues(t, 123, o.ValueOrZero())
	})
}

func TestOf_UnmarshalJSON(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalJSON([]byte("null"))
		if assert.NoError(t, err) {
			assert.False(t, o.Valid)
		}
	})
	t.Run("bool, true", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalJSON([]byte("true"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.True(t, o.V)
		}
	})
	t.Run("bool, false", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalJSON([]byte("false"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.False(t, o.V)
		}
	})
	t.Run("int", func(t *testing.T) {
		var o of.O[int]
		err := o.UnmarshalJSON([]byte("123"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, 123, o.V)
		}
	})
	t.Run("string", func(t *testing.T) {
		var o of.O[string]
		err := o.UnmarshalJSON([]byte("\"Hello\""))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, "Hello", o.V)
		}
	})
	t.Run("time.Time", func(t *testing.T) {
		var o of.O[time.Time]
		now, err := time.Parse(time.RFC3339, "2022-10-10T14:12:02Z")
		require.NoError(t, err)
		err = o.UnmarshalJSON([]byte("\"" + now.Format(time.RFC3339) + "\""))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, now, o.V)
		}
	})
	t.Run("uuid.UUID", func(t *testing.T) {
		var o of.O[uuid.UUID]
		err := o.UnmarshalJSON([]byte("\"b3b6173c-6dd4-45a6-bcb8-9b74acb037be\""))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, "b3b6173c-6dd4-45a6-bcb8-9b74acb037be", o.V.String())
		}
	})
}

func TestOf_MarshalJSON(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		var o of.O[bool]
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "null", v)
		}
	})
	t.Run("bool, true", func(t *testing.T) {
		o := of.From(true)
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "true", v)
		}
	})
	t.Run("bool, false", func(t *testing.T) {
		o := of.From(false)
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "false", v)
		}
	})
	t.Run("int", func(t *testing.T) {
		o := of.From(123)
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "123", v)
		}
	})
	t.Run("string", func(t *testing.T) {
		o := of.From("World")
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "\"World\"", v)
		}
	})
	t.Run("time.Time", func(t *testing.T) {
		now := time.Now()
		o := of.From(now)
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "\""+now.Format(time.RFC3339Nano)+"\"", v)
		}
	})
	t.Run("uuid.UUID", func(t *testing.T) {
		id := uuid.Must(uuid.New(), nil)
		o := of.From(id)
		v, err := o.MarshalJSON()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "\""+id.String()+"\"", v)
		}
	})
}

func TestOf_UnmarshalText(t *testing.T) {
	t.Run("invalid, empty", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalText([]byte{})
		if assert.NoError(t, err) {
			assert.False(t, o.Valid)
		}
	})
	t.Run("invalid, null", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalText([]byte("null"))
		if assert.NoError(t, err) {
			assert.False(t, o.Valid)
		}
	})
	t.Run("bool, true", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalText([]byte("true"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.True(t, o.V)
		}
	})
	t.Run("bool, false", func(t *testing.T) {
		var o of.O[bool]
		err := o.UnmarshalText([]byte("false"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.False(t, o.V)
		}
	})
	t.Run("int", func(t *testing.T) {
		var o of.O[int]
		err := o.UnmarshalText([]byte("123"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, 123, o.V)
		}
	})
	t.Run("string", func(t *testing.T) {
		var o of.O[string]
		err := o.UnmarshalText([]byte("Hello"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, "Hello", o.V)
		}
	})
	t.Run("time.Time", func(t *testing.T) {
		var o of.O[time.Time]
		now, err := time.Parse(time.RFC3339, "2022-10-10T14:12:02Z")
		require.NoError(t, err)
		err = o.UnmarshalText([]byte("2022-10-10T14:12:02Z"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, now, o.V)
		}
	})
	t.Run("uuid.UUID", func(t *testing.T) {
		var o of.O[uuid.UUID]
		err := o.Scan([]byte("b3b6173c-6dd4-45a6-bcb8-9b74acb037be"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, "b3b6173c-6dd4-45a6-bcb8-9b74acb037be", o.V.String())
		}
	})
}

func TestOf_Scan(t *testing.T) {
	t.Run("bool, true", func(t *testing.T) {
		var o of.O[bool]
		err := o.Scan([]byte("true"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.True(t, o.V)
		}
	})
	t.Run("bool, false", func(t *testing.T) {
		var o of.O[bool]
		err := o.Scan([]byte("false"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.False(t, o.V)
		}
	})
	t.Run("int", func(t *testing.T) {
		var o of.O[int]
		err := o.Scan([]byte("123"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, 123, o.V)
		}
	})
	t.Run("string", func(t *testing.T) {
		var o of.O[string]
		err := o.Scan([]byte("Hello"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, "Hello", o.V)
		}
	})
	t.Run("time.Time", func(t *testing.T) {
		var o of.O[time.Time]
		now := time.Now()
		err := o.Scan(now)
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, now, o.V)
		}
	})
	t.Run("uuid.UUID", func(t *testing.T) {
		var o of.O[uuid.UUID]
		err := o.Scan([]byte("b3b6173c-6dd4-45a6-bcb8-9b74acb037be"))
		if assert.NoError(t, err) {
			assert.True(t, o.Valid)
			assert.EqualValues(t, "b3b6173c-6dd4-45a6-bcb8-9b74acb037be", o.V.String())
		}
	})
}

func TestOf_MarshalText(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		var o of.O[bool]
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.Len(t, v, 0)
		}
	})
	t.Run("bool, true", func(t *testing.T) {
		o := of.From(true)
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "true", v)
		}
	})
	t.Run("bool, false", func(t *testing.T) {
		o := of.From(false)
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "false", v)
		}
	})
	t.Run("int", func(t *testing.T) {
		o := of.From(123)
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "123", v)
		}
	})
	t.Run("string", func(t *testing.T) {
		o := of.From("World")
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "World", v)
		}
	})
	t.Run("time.Time", func(t *testing.T) {
		now := time.Now()
		o := of.From(now)
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.EqualValues(t, now.Format(time.RFC3339Nano), v)
		}
	})
	t.Run("uuid.UUID", func(t *testing.T) {
		id := uuid.Must(uuid.New(), nil)
		o := of.From(id)
		v, err := o.MarshalText()
		if assert.NoError(t, err) {
			assert.EqualValues(t, id.String(), v)
		}
	})
}

func TestOf_Value(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		var o of.O[bool]
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.Nil(t, v)
		}
	})
	t.Run("bool, true", func(t *testing.T) {
		o := of.From(true)
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.EqualValues(t, true, v)
		}
	})
	t.Run("bool, false", func(t *testing.T) {
		o := of.From(false)
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.EqualValues(t, false, v)
		}
	})
	t.Run("int", func(t *testing.T) {
		o := of.From(123)
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.EqualValues(t, 123, v)
			assert.IsType(t, int64(123), v)
		}
	})
	t.Run("string", func(t *testing.T) {
		o := of.From("World")
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.EqualValues(t, "World", v)
		}
	})
	t.Run("time.Time", func(t *testing.T) {
		now := time.Now()
		o := of.From(now)
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.EqualValues(t, now, v)
		}
	})
	t.Run("uuid.UUID", func(t *testing.T) {
		id := uuid.Must(uuid.New(), nil)
		o := of.From(id)
		v, err := o.Value()
		if assert.NoError(t, err) {
			assert.EqualValues(t, id.String(), v)
		}
	})
}

func TestOf(t *testing.T) {
	t.Run("Of", func(t *testing.T) {
		o := of.Of(1, true)
		t.Logf("%v", o.Val())
	})
	t.Run("OfPtr", func(t *testing.T) {
		o := of.OfPtr((*int)(nil))
		t.Logf("%v", o.Val())
	})
	t.Run("OfPtr", func(t *testing.T) {
		o := of.OfPtr(ptr.Of(123))
		t.Logf("%v", o.Val())
	})
}

func TestO_ValueOr(t *testing.T) {
	assert.Equal(t, 10, of.OK(10).ValueOr(1))
	assert.Equal(t, 1, of.Nil[int]().ValueOr(1))
	assert.Equal(t, 10, of.Of(10, true).ValueOr(1))
	assert.Equal(t, 1, of.Of(10, false).ValueOr(1))
	assert.Equal(t, 1, of.Of(0, false).ValueOr(1))
}

func TestOValue(t *testing.T) {
	assert.Equal(t, 10, of.OK(10).Val())
	assert.Equal(t, 0, of.Nil[int]().Val())
	assert.Equal(t, 10, of.Of(10, true).Val())
	assert.Equal(t, 10, of.Of(10, false).Val()) // 💡 NOTE: not recommend
	assert.Equal(t, 0, of.Of(0, false).Val())

	assert.Equal(t, 1, of.OfPtr(ptr.Of(1)).Val())
	assert.Equal(t, 0, of.OfPtr((*int)(nil)).Val())
}

func TestOValueOrZero(t *testing.T) {
	assert.Equal(t, 10, of.OK(10).ValueOrZero())
	assert.Equal(t, 0, of.Nil[int]().ValueOrZero())
	assert.Equal(t, 10, of.Of(10, true).ValueOrZero())
	assert.Equal(t, 0, of.Of(10, false).ValueOrZero()) // 💡 NOTE: not recommend
	assert.Equal(t, 0, of.Of(0, false).ValueOrZero())
}

func TestOOK(t *testing.T) {
	assert.True(t, of.OK(10).IsOK())
	assert.True(t, of.OK(0).IsOK())
	assert.False(t, of.Nil[int]().IsOK())
	assert.True(t, of.Of(10, true).IsOK())
	assert.False(t, of.Of(10, false).IsOK()) // 💡 NOTE: not recommend
	assert.False(t, of.Of(0, false).IsOK())
}

func TestOIfOK(t *testing.T) {
	assert.True(t, of.OK(10).IsOK())
	assert.True(t, of.OK(0).IsOK())
	assert.False(t, of.Nil[int]().IsOK())
}

func TestOIfNil(t *testing.T) {
	assert.True(t, of.OK(10).IsNil())
	assert.True(t, of.OK(0).IsNil())
	assert.False(t, of.Nil[int]().IsNil())
}

func TestOty(t *testing.T) {
	assert.Equal(t, "any", of.Nil[any]().Type())
	assert.Equal(t, "int", of.Nil[int]().Type())
	assert.Equal(t, "int", of.OK(11).Type())
	assert.Equal(t, "int8", of.OK(int8(11)).Type())
	assert.Equal(t, "any", of.OK(any(11)).Type())
	assert.Equal(t, "any", of.OK[any](11).Type())
	assert.Equal(t, "any", of.OK[interface{}](11).Type())
	assert.Equal(t, "any", of.OK((interface{})(11)).Type())
}

func TestOString(t *testing.T) {
	assert.Equal(t, "goption.Nil[int]()", of.O[int]{}.String())
	assert.Equal(t, "goption.Nil[int]()", of.Nil[int]().String())
	assert.Equal(t, "goption.OK[int](11)", of.OK(11).String())
	assert.Equal(t, "goption.OK[any](11)", of.OK(any(11)).String())
	assert.Equal(t, "goption.OK[int](11)", fmt.Sprintf("%s", of.OK(11)))
}

func TestJSON(t *testing.T) {
	{
		var v *int
		expect, _ := json.Marshal(v)
		actual, _ := json.Marshal(of.OfPtr(v))
		assert.Equal(t, string(expect), string(actual))
	}
	{
		v := ptr.Of(1)
		expect, _ := json.Marshal(v)
		actual, _ := json.Marshal(of.OfPtr(v))
		assert.Equal(t, string(expect), string(actual))
	}
	{
		v := ptr.Of("test")
		expect, _ := json.Marshal(v)
		actual, _ := json.Marshal(of.OfPtr(v))
		assert.Equal(t, string(expect), string(actual))
	}

	// Simple.
	{
		bs, err := json.Marshal(of.OK("test"))
		assert.Nil(t, err)
		assert.Equal(t, `"test"`, string(bs))
	}
	{
		bs, err := json.Marshal(of.Nil[string]())
		assert.Nil(t, err)
		assert.Equal(t, `null`, string(bs))
	}

	{ // Bidirect
		before := of.OK("test")
		bs, err := json.Marshal(before)
		assert.Nil(t, err)

		var after1 of.O[int]
		err = json.Unmarshal(bs, &after1)
		assert.NotNil(t, err)
		assert.Equal(t, of.Nil[int](), after1)

		var after2 of.O[float64]
		err = json.Unmarshal(bs, &after2)
		assert.NotNil(t, err)
		assert.Equal(t, of.Nil[float64](), after2)

		var after3 of.O[string]
		err = json.Unmarshal(bs, &after3)
		assert.Nil(t, err)
		assert.Equal(t, before, after3)
	}

	{ // Unmarshal
		var o of.O[string]
		err := json.Unmarshal([]byte(`"test"`), &o)
		assert.Nil(t, err)
		assert.Equal(t, of.OK("test"), o)
	}
	{ // Unmarshal nil
		var o of.O[string]
		err := json.Unmarshal([]byte(`null`), &o)
		assert.Nil(t, err)
		assert.Equal(t, of.Nil[string](), o)
	}

	// Struct field
	{
		type Foo struct {
			Bar of.O[int] `json:"bar"`
		}

		foo1 := Foo{of.OK(0)}
		bs1, err := json.Marshal(foo1)
		assert.Nil(t, err)
		assert.Equal(t, `{"bar":0}`, string(bs1))

		foo2 := Foo{}
		bs2, err := json.Marshal(foo2)
		assert.Nil(t, err)
		assert.Equal(t, `{"bar":null}`, string(bs2))

		foo3 := Foo{}
		err = json.Unmarshal(bs1, &foo3)
		assert.Nil(t, err)
		assert.Equal(t, foo1, foo3)

		foo4 := Foo{}
		err = json.Unmarshal(bs2, &foo4)
		assert.Nil(t, err)
		assert.Equal(t, foo2, foo4)

		type Fooo struct {
			Bar *of.O[int] `json:"bar"`
		}

		foo5 := Fooo{}
		err = json.Unmarshal(bs1, &foo5)
		assert.Nil(t, err)
		assert.Equal(t, foo1.Bar, *foo5.Bar)

		foo6 := Fooo{}
		err = json.Unmarshal(bs2, &foo6)
		assert.Nil(t, err)
		assert.True(t, foo6.Bar == nil)
	}
}

func TestOIsOK(t *testing.T) {
	assert.True(t, of.OK(0).IsOK())
	assert.False(t, of.Nil[int]().IsOK())
	assert.True(t, of.Of(10, true).IsOK())
	assert.False(t, of.Of(10, false).IsOK()) // 💡 NOTE: not recommend
	assert.False(t, of.Of(0, false).IsOK())
}

func TestOIsNil(t *testing.T) {
	assert.False(t, of.OK(10).IsNil())
	assert.False(t, of.OK(0).IsNil())
	assert.True(t, of.Nil[int]().IsNil())
	assert.False(t, of.Of(10, true).IsNil())
	assert.True(t, of.Of(10, false).IsNil()) // 💡 NOTE: not recommend
	assert.True(t, of.Of(0, false).IsNil())
}

func TestO_Alias(t *testing.T) {
	assert.True(t, of.OK(10).IsOK())
	assert.False(t, of.Nil[int]().IsOK())
	assert.True(t, of.Of(10, true).IsOK())
}

func TestOMap(t *testing.T) {
	assert.Equal(t, of.OK("1"), of.Map(of.OK(1), strconv.Itoa))
	assert.Equal(t, of.Nil[string](), of.Map(of.Nil[int](), strconv.Itoa))

	f := func(v int) string { panic("function should not be called") }
	assert.Equal(t, of.Nil[string](), of.Map(of.Nil[int](), f))
}

func TestOThen(t *testing.T) {
	do := func(v int) of.O[string] {
		return of.OK(strconv.Itoa(v))
	}
	doNil := func(v int) of.O[string] {
		return of.Nil[string]()
	}
	assert.Equal(t, of.OK("1"), of.Then(of.OK(1), do))
	assert.Equal(t, of.Nil[string](), of.Then(of.OK(1), doNil))
	assert.Equal(t, of.Nil[string](), of.Then(of.Nil[int](), do))
	assert.Equal(t, of.Nil[string](), of.Then(of.Nil[int](), doNil))

	f := func(v int) of.O[string] { panic("function should not be called") }
	assert.Equal(t, of.Nil[string](), of.Then(of.Nil[int](), f))
}

func TestOPtr(t *testing.T) {
	assert.Equal(t, ptr.Of(10), of.OK(10).Ptr())
	assert.Equal(t, true, of.Nil[int]().IsNil())

	// Test modify.
	{
		o := of.OK(10)
		ptr := o.Ptr()
		*ptr = 1
		assert.Equal(t, of.OK(10), o)
		assert.True(t, o.Ptr() != o.Ptr()) // o is copied
	}
}
