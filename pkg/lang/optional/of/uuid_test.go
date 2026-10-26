package of_test

import (
	"database/sql/driver"
	"testing"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/pkg/lang/optional/of"
)

func TestUUIDScan(t *testing.T) {
	id := uuid.MustParse("b3b6173c-6dd4-45a6-bcb8-9b74acb037be")
	for _, tc := range []struct {
		name string
		src  any
	}{
		{"string", id.String()},
		{"text bytes", []byte(id.String())},
		{"binary bytes", id[:]},
		{"urn", "urn:uuid:" + id.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var o of.O[uuid.UUID]
			require.NoError(t, o.Scan(tc.src))
			require.Equal(t, of.From(id), o)
		})
	}
	t.Run("null and empty inputs clear a previous value", func(t *testing.T) {
		for _, src := range []any{nil, "", []byte{}, []byte(nil)} {
			o := of.From(id)
			require.NoError(t, o.Scan(src))
			require.Equal(t, of.Nil[uuid.UUID](), o)
		}
	})
	t.Run("errors leave the optional unchanged", func(t *testing.T) {
		for _, src := range []any{"invalid", []byte("invalid"), 123} {
			for _, initial := range []of.O[uuid.UUID]{of.From(id), of.Nil[uuid.UUID]()} {
				o := initial
				require.Error(t, o.Scan(src))
				require.Equal(t, initial, o)
			}
		}
	})
	t.Run("zero UUID is a valid value", func(t *testing.T) {
		var o of.O[uuid.UUID]
		require.NoError(t, o.Scan(uuid.Nil().String()))
		require.Equal(t, of.From(uuid.Nil()), o)
	})
}

func TestUUIDSQLValue(t *testing.T) {
	for _, id := range []uuid.UUID{uuid.New(), uuid.Nil()} {
		v, err := of.From(id).Value()
		require.NoError(t, err)
		require.Equal(t, id.String(), v)
		require.True(t, driver.IsValue(v))
	}
	v, err := of.Nil[uuid.UUID]().Value()
	require.NoError(t, err)
	require.Nil(t, v)
}
