package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStoreLifecycle(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	ok, err := s.CreateSession(ctx, "sid", "g1", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// Creation is compare-and-set: a live session is never overwritten.
	ok, err = s.CreateSession(ctx, "sid", "g2", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = s.MatchSession(ctx, "sid", "g1")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.MatchSession(ctx, "sid", "other")
	require.NoError(t, err)
	require.False(t, ok)

	// A stale generation cannot rotate; the current one can.
	ok, err = s.RotateSession(ctx, "sid", "stale", "g2", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.RotateSession(ctx, "sid", "g1", "g2", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, s.DeleteSession(ctx, "sid"))
	ok, err = s.MatchSession(ctx, "sid", "g2")
	require.NoError(t, err)
	require.False(t, ok)
	// Deleted sessions are not resurrected by rotation.
	ok, err = s.RotateSession(ctx, "sid", "g2", "g3", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemoryStoreExpiry(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	_, err := s.CreateSession(ctx, "sid", "g", 10*time.Millisecond)
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)

	ok, err := s.MatchSession(ctx, "sid", "g")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.RotateSession(ctx, "sid", "g", "next", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	// An expired id can be created again (it is a fresh session id in practice).
	ok, err = s.CreateSession(ctx, "sid", "g2", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestMemoryStoreRejectsBadTTL(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	_, err := s.CreateSession(ctx, "sid", "g", 0)
	assert.Error(t, err)
	_, err = s.RotateSession(ctx, "sid", "old", "next", 0)
	assert.Error(t, err)
}

func TestMemoryStoreConcurrentRotation(t *testing.T) {
	s := NewStore()
	ctx := context.Background()
	_, err := s.CreateSession(ctx, "sid", "g0", time.Minute)
	require.NoError(t, err)

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.RotateSession(ctx, "sid", "g0", fmt.Sprintf("next-%d", i), time.Minute)
			if err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
}
