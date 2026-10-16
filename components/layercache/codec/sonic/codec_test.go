package sonic

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	in := struct{ Name string }{"value"}
	b, err := (Codec{}).Marshal(in)
	require.NoError(t, err)
	var out struct{ Name string }
	require.NoError(t, (Codec{}).Unmarshal(b, &out))
	require.Equal(t, in, out)
}
