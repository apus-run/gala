package layercache

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSerializedPayloadInteroperability(t *testing.T) {
	type record struct {
		N int `json:"n"`
	}
	c := newCache[record](t, newLocal(), newRemote())
	require.NoError(t, c.Set(ctx, "object", record{N: 42}))
	var raw []byte
	require.NoError(t, c.Cache.Get(ctx, "object", &raw))
	require.JSONEq(t, `{"n":42}`, string(raw))
	raw[0] = 'X'
	var text string
	require.NoError(t, c.Cache.Get(ctx, "object", &text))
	require.JSONEq(t, `{"n":42}`, text, "raw reads must not mutate cached bytes")

	for _, input := range []any{[]byte(text), text} {
		require.NoError(t, c.Cache.Set(ctx, "encoded", Value(input)))
		require.NoError(t, c.InvalidateLocal(ctx, "encoded"))
		got, err := c.Get(ctx, "encoded")
		require.NoError(t, err)
		require.Equal(t, record{N: 42}, got)
		var once record
		require.NoError(t, c.Once(ctx, "encoded", Value(&once)))
		require.Equal(t, got, once)
	}

	// Raw destinations still bypass the codec, including invalid encoded data.
	require.NoError(t, c.Cache.Set(ctx, "bad", Value([]byte("{bad"))))
	_, err := c.Get(ctx, "bad")
	require.ErrorIs(t, err, ErrCorrupt)
	require.NoError(t, c.Cache.Get(ctx, "bad", &raw))
	require.Equal(t, []byte("{bad"), raw)
}

// Deliberately ignores TTL to verify the coordinator's own deadline check.
type delayedLocal struct {
	entry  Entry
	exists bool
	delay  time.Duration
	ttl    time.Duration
	sets   int
}

func (l *delayedLocal) Get(string) (Entry, bool) { return l.entry, l.exists }
func (l *delayedLocal) Set(_ string, e Entry, ttl time.Duration) {
	l.sets++
	time.Sleep(l.delay)
	l.entry = e
	l.exists = true
	l.ttl = ttl
}
func (l *delayedLocal) Remove(string) bool { had := l.exists; l.exists = false; return had }

func TestLocalTTLDoesNotOutliveRemote(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		c := newCache[string](t, newLocal(), r, WithLocalTTL(time.Minute))
		data, err := c.encode("value", false)
		require.NoError(t, err)
		deadline := time.Now().Add(150 * time.Millisecond)
		r.data[c.prefix+"key"] = RemoteEntry{Data: data, ExpiresAt: deadline}
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", got)
		entry, ok := c.local.Get(c.prefix + "key")
		require.True(t, ok)
		require.Equal(t, deadline, entry.ExpiresAt, "[ablation:read_deadline]")
		time.Sleep(150 * time.Millisecond)
		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss)
	})
}

func TestDelayedLocalSetCannotRestartTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		local := &delayedLocal{delay: 2 * time.Second}
		c := newCache[string](t, local, nil, WithLocalTTL(time.Second))
		require.NoError(t, c.Set(ctx, "key", "value"))
		require.Equal(t, time.Second, local.ttl)
		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss, "[ablation:local_deadline]")
	})
}

func TestExpiredRemoteNeverWritesZeroTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		local := &delayedLocal{}
		r := newRemote()
		r.get = func(context.Context, string) (RemoteEntry, error) {
			return RemoteEntry{Data: []byte{'L', 'C', 1, 0, '1'}, ExpiresAt: time.Now()}, nil
		}
		c := newCache[string](t, local, r)
		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss)
		require.Zero(t, local.sets)
	})
}

// Models a backend with millisecond precision, as used by Redis SET PX.
type roundedRemote struct{ *memRemote }

func (r roundedRemote) Set(ctx context.Context, key string, data []byte, ttl time.Duration) (time.Time, error) {
	return r.memRemote.Set(ctx, key, data, ttl/time.Millisecond*time.Millisecond)
}

func TestWrittenLocalEntryCannotOutliveRoundedRemoteTTL(t *testing.T) {
	for _, operation := range []string{"Set", "GetOrLoad", "negative"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := roundedRemote{newRemote()}
				c := newCache[string](t, newLocal(), r, WithRemoteTTL(1500*time.Microsecond), WithNotFoundTTL(1500*time.Microsecond))
				switch operation {
				case "Set":
					require.NoError(t, c.Set(ctx, "key", "value"))
				case "GetOrLoad":
					_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "value", nil })
					require.NoError(t, err)
				case "negative":
					_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "", ErrNotFound })
					require.ErrorIs(t, err, ErrNotFound)
				}
				time.Sleep(1200 * time.Microsecond)
				_, err := r.Get(ctx, c.prefix+"key")
				require.ErrorIs(t, err, ErrMiss)
				_, err = c.Get(ctx, "key")
				require.ErrorIs(t, err, ErrMiss, "[ablation:write_receipt]")
			})
		})
	}
}

type receiptRemote struct {
	*memRemote
	receipt func(time.Time, time.Duration) time.Time
}

func (r receiptRemote) Set(ctx context.Context, key string, data []byte, ttl time.Duration) (time.Time, error) {
	started := time.Now()
	_, err := r.memRemote.Set(ctx, key, data, ttl)
	return r.receipt(started, ttl), err
}

func TestWriteDeadlineValidationAndLatency(t *testing.T) {
	for _, scenario := range []string{"missing", "elapsed", "too long", "response latency"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := time.Now()
				r := receiptRemote{memRemote: newRemote(), receipt: func(start time.Time, ttl time.Duration) time.Time {
					switch scenario {
					case "missing":
						return time.Time{}
					case "elapsed":
						return start
					case "too long":
						return start.Add(2 * ttl)
					default:
						time.Sleep(20 * time.Millisecond)
						return start.Add(50 * time.Millisecond)
					}
				}}
				c := newCache[string](t, newLocal(), r, WithRemoteTTL(100*time.Millisecond))
				err := c.Set(ctx, "key", "value")
				if scenario == "missing" {
					require.ErrorIs(t, err, ErrInvalidTTL)
				} else {
					require.NoError(t, err)
				}
				entry, ok := c.local.Get(c.prefix + "key")
				switch scenario {
				case "missing", "elapsed":
					require.False(t, ok)
				case "too long":
					require.True(t, ok)
					require.Equal(t, started.Add(100*time.Millisecond), entry.ExpiresAt, "[ablation:requested_ttl_cap]")
				case "response latency":
					require.True(t, ok)
					require.Equal(t, 30*time.Millisecond, time.Until(entry.ExpiresAt))
				}
			})
		})
	}
}

type customCodec struct {
	payload   []byte
	decodeErr error
}

func (c customCodec) Marshal(any) ([]byte, error) { return c.payload, nil }
func (c customCodec) Unmarshal(data []byte, v any) error {
	if c.decodeErr != nil {
		return c.decodeErr
	}
	*v.(*string) = string(data)
	return nil
}

func TestCodecPayloadCannotImpersonateNegativeRecord(t *testing.T) {
	for _, payload := range [][]byte{[]byte("*"), {'L', 'C', 1, 1}} {
		c := newCache[string](t, nil, newRemote(), WithCodec(customCodec{payload: payload}))
		require.NoError(t, c.Cache.Set(ctx, "key", Value(struct{}{})))
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, string(payload), got)
	}
	c := newCache[string](t, newLocal(), nil, WithCodec(customCodec{}))
	require.ErrorContains(t, c.Cache.Set(ctx, "key", Value(struct{}{})), "empty payload")
}

func TestDecodeErrorsWrappingMissNeverLoad(t *testing.T) {
	r := newRemote()
	r.get = func(context.Context, string) (RemoteEntry, error) {
		return RemoteEntry{Data: []byte{'L', 'C', 1, 0, 'x'}, ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	c := newCache[int](t, nil, r, WithCodec(customCodec{decodeErr: ErrMiss}))
	called := false
	_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (int, error) { called = true; return 0, nil })
	require.ErrorIs(t, err, ErrCorrupt)
	require.False(t, called)
}

func FuzzDecode(f *testing.F) {
	for _, seed := range [][]byte{nil, {'L', 'C', 1, 1}, {'L', 'C', 1, 0}, []byte("LC\x01\x00null"), []byte("LC\x02\x00{}"), []byte("*")} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		kind, _, err := envelope(data)
		if err == nil && kind == 1 {
			require.Len(t, data, 4)
			require.Equal(t, byte(1), data[3])
		}
	})
}

// Invalid JSON from a custom Marshaler must not be published by the default codec.
type invalidJSON struct{}

func (invalidJSON) MarshalJSON() ([]byte, error) { return []byte("not-json"), nil }

func TestDefaultCodecRejectsInvalidMarshalJSON(t *testing.T) {
	r := newRemote()
	c := newCache[any](t, newLocal(), r)
	require.NoError(t, c.Set(ctx, "key", "old"))
	writes := r.writes.Load()
	require.Error(t, c.Set(ctx, "key", invalidJSON{}))
	require.Equal(t, writes, r.writes.Load())
	got, err := c.Get(ctx, "key")
	require.NoError(t, err)
	require.Equal(t, "old", got)
}

func TestCorruptCodecErrorIsNotAuthoritativeAbsence(t *testing.T) {
	r := newRemote()
	r.get = func(context.Context, string) (Entry, error) {
		return Entry{Data: []byte{'L', 'C', 2, 0, 'x'}, ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	c := newCache[int](t, newLocal(), r, WithCodec(customCodec{decodeErr: ErrNotFound}))
	_, err := c.Get(ctx, "key")
	require.ErrorIs(t, err, ErrCorrupt)
	require.False(t, c.IsNotFound(err))
	var op *OpError
	require.ErrorAs(t, err, &op)
}
