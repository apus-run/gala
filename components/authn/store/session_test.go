package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisSessionRotation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	s := NewStore(client, "auth:")
	ctx := context.Background()
	ok, err := s.CreateSession(ctx, "sid", "old", time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.CreateSession(ctx, "sid", "different", time.Hour)
	require.NoError(t, err)
	require.False(t, ok)
	var wg sync.WaitGroup
	var wins atomic.Int32
	for range 16 {
		wg.Go(func() {
			ok, err := s.RotateSession(ctx, "sid", "old", "new", 2*time.Hour)
			if err != nil {
				t.Errorf("rotate: %v", err)
			}
			if ok {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
	ok, err = s.MatchSession(ctx, "sid", "old")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.MatchSession(ctx, "sid", "new")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2*time.Hour, server.TTL("auth:session:sid"))
	require.NoError(t, s.DeleteSession(ctx, "sid"))
	ok, err = s.RotateSession(ctx, "sid", "new", "resurrect", time.Hour)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.MatchSession(ctx, "sid", "new")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.CreateSession(ctx, "expiring", "g", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	server.FastForward(2 * time.Second)
	ok, err = s.RotateSession(ctx, "expiring", "g", "next", time.Hour)
	require.NoError(t, err)
	require.False(t, ok)
	_, err = s.CreateSession(ctx, "bad", "g", 0)
	require.Error(t, err)
	_, err = s.RotateSession(ctx, "bad", "g", "next", 0)
	require.Error(t, err)
	server.SetError("storage unavailable")
	_, err = s.MatchSession(ctx, "sid", "new")
	require.Error(t, err)
	_, err = s.CreateSession(ctx, "error", "g", time.Hour)
	require.Error(t, err)
	_, err = s.RotateSession(ctx, "sid", "new", "next", time.Hour)
	require.Error(t, err)
	require.Error(t, s.DeleteSession(ctx, "sid"))
}
