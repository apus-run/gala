package msgpack

import (
	"bytes"
	"encoding/binary"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestThresholdAndRoundTrip(t *testing.T) {
	for _, n := range []int{1, 256} {
		value := struct{ Text string }{strings.Repeat("x", n)}
		b, err := (Codec{}).Marshal(value)
		require.NoError(t, err)
		kind := byte(0)
		if n == 256 {
			kind = 1
		}
		require.Equal(t, kind, b[len(b)-1], "[ablation:compression_threshold]")
		var got struct{ Text string }
		require.NoError(t, (Codec{}).Unmarshal(b, &got))
		require.Equal(t, value, got)
	}
}
func TestMalformedAndBoundedDecompression(t *testing.T) {
	var value any
	for _, b := range [][]byte{nil, {2}, {255, 1}} {
		require.Error(t, (Codec{}).Unmarshal(b, &value))
	}
	bomb, err := (Codec{}).Marshal(strings.Repeat("x", 4096))
	require.NoError(t, err)
	require.Error(t, (Codec{MaxDecodedBytes: 64}).Unmarshal(bomb, &value), "[ablation:decompression_limit]")
	_, err = (Codec{MaxDecodedBytes: 8}).Marshal(strings.Repeat("x", 64))
	require.Error(t, err)
}
func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		var v any
		_ = (Codec{MaxDecodedBytes: 4096}).Unmarshal(b, &v)
		var m map[string]any
		_ = (Codec{MaxDecodedBytes: 4096}).Unmarshal(b, &m)
		var a map[any]any
		_ = (Codec{MaxDecodedBytes: 4096}).Unmarshal(b, &a)
	})
}

func TestNestingIsBounded(t *testing.T) {
	data := append(bytes.Repeat([]byte{0x91}, 80), 0xc0, 0)
	var value any
	require.Error(t, (Codec{}).Unmarshal(data, &value), "[ablation:nesting_limit]")
}
func TestContainerHeadersAreValidatedBeforeAllocation(t *testing.T) {
	for _, data := range [][]byte{{0xdd, 255, 255, 255, 255}, {0xdf, 255, 255, 255, 255}, {0xc1}, {0xc0, 0xc0}} {
		require.Error(t, validate(data))
	}
}

func TestMarshalAlsoRejectsExcessiveNesting(t *testing.T) {
	var value any
	for range 80 {
		value = []any{value}
	}
	_, err := (Codec{}).Marshal(value)
	require.Error(t, err)
}

func TestExtensionCannotSmuggleMapAllocation(t *testing.T) {
	// Test the scan/reader boundary, never invoke the vulnerable allocating
	// decoder directly. Both a root map and a struct's map field are protected.
	payload := []byte{0xc7, 5, 0, 0xdf, 255, 255, 255, 255, 0}
	var value map[string]any
	require.ErrorContains(t, (Codec{}).Unmarshal(payload, &value), "extension body")
	nested := append([]byte{0x81, 0xa1, 'M'}, payload...)
	var record struct{ M map[string]any }
	require.ErrorContains(t, (Codec{}).Unmarshal(nested, &record), "extension body")
}
func TestTimestampWithMapLikeBytesRemainsValid(t *testing.T) {
	// This valid 64-bit timestamp starts with a map32-looking byte sequence.
	wire := []byte{0xd7, 0xff, 0xdf, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}
	bits := binary.BigEndian.Uint64(wire[2:10])
	want := time.Unix(int64(bits&0x00000003ffffffff), int64(bits>>34))
	var got time.Time
	require.NoError(t, (Codec{}).Unmarshal(wire, &got))
	require.True(t, want.Equal(got))
	b, err := (Codec{}).Marshal(want)
	require.NoError(t, err)
	require.NoError(t, (Codec{}).Unmarshal(b, &got))
	require.True(t, want.Equal(got))
	var badTarget map[string]any
	require.ErrorContains(t, (Codec{}).Unmarshal(wire, &badTarget), "extension body")
}

func TestOpaqueExtensionReader(t *testing.T) {
	// Small declared size makes this a safe ablation oracle even with the reader
	// guard removed. The separate regression covers the huge-header attack.
	b := []byte{0xc7, 5, 0, 0xdf, 0, 0, 0, 2, 0}
	var value map[string]any
	require.ErrorIs(t, (Codec{}).Unmarshal(b, &value), errExtensionBody, "[ablation:extension_boundary]")
}
func TestUnhashableMapKeyReturnsError(t *testing.T) {
	// map { []: nil } is syntactically MessagePack but cannot be a Go map key.
	var value map[any]any
	require.Error(t, (Codec{}).Unmarshal([]byte{0x81, 0x90, 0xc0, 0}, &value))
}
