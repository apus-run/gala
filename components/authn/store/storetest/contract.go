// Package storetest provides a contract test suite for SessionStorer
// implementations. Third-party stores should run it to prove the atomicity and
// no-resurrection rules that SessionAuth depends on.
// Package storetest 为 SessionStorer 实现提供一致性测试套件。第三方存储应运行它，
// 证明其满足 SessionAuth 依赖的原子性与“不复活”规则。
package storetest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/authn/store"
)

// Clock advances the backend's notion of time, so the expiry case does not
// need a real sleep on backends that can fast-forward.
// Clock 推进后端的时间；能快进的后端不必真实等待即可测试过期。
type Clock interface {
	Advance(d time.Duration)
}

// Options configures the suite. New is called once per case and must return an
// isolated store; the caller owns any cleanup.
// Options 配置套件。New 每个用例调用一次，必须返回隔离的存储；清理由调用方负责。
type Options struct {
	New func(t *testing.T) (store.SessionStorer, Clock)
}

// SessionStorer runs the full contract.
// SessionStorer 运行完整契约。
func SessionStorer(t *testing.T, opts Options) {
	t.Helper()
	ctx := context.Background()

	t.Run("create is compare-and-set", func(t *testing.T) {
		s, _ := opts.New(t)
		ok, err := s.CreateSession(ctx, "sid", "g1", time.Minute)
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = s.CreateSession(ctx, "sid", "g2", time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "a live session must not be overwritten")

		ok, err = s.MatchSession(ctx, "sid", "g1")
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = s.MatchSession(ctx, "sid", "g2")
		require.NoError(t, err)
		require.False(t, ok)
		ok, err = s.MatchSession(ctx, "missing", "g1")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("rotate is compare-and-set", func(t *testing.T) {
		s, _ := opts.New(t)
		_, err := s.CreateSession(ctx, "sid", "g1", time.Minute)
		require.NoError(t, err)

		ok, err := s.RotateSession(ctx, "sid", "stale", "g2", time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "a stale generation must not rotate")
		ok, err = s.MatchSession(ctx, "sid", "g1")
		require.NoError(t, err)
		require.True(t, ok, "a failed rotation must leave the current generation intact")

		ok, err = s.RotateSession(ctx, "sid", "g1", "g2", time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		ok, err = s.MatchSession(ctx, "sid", "g1")
		require.NoError(t, err)
		require.False(t, ok)
		ok, err = s.MatchSession(ctx, "sid", "g2")
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("deleted sessions are not resurrected", func(t *testing.T) {
		s, _ := opts.New(t)
		_, err := s.CreateSession(ctx, "sid", "g1", time.Minute)
		require.NoError(t, err)
		require.NoError(t, s.DeleteSession(ctx, "sid"))
		require.NoError(t, s.DeleteSession(ctx, "sid"), "delete must be idempotent")

		ok, err := s.MatchSession(ctx, "sid", "g1")
		require.NoError(t, err)
		require.False(t, ok)
		ok, err = s.RotateSession(ctx, "sid", "g1", "g2", time.Minute)
		require.NoError(t, err)
		require.False(t, ok, "rotation must not recreate a deleted session")

		// A genuinely new session with the same id may be created.
		ok, err = s.CreateSession(ctx, "sid", "g3", time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("expired sessions are not resurrected", func(t *testing.T) {
		s, clock := opts.New(t)
		_, err := s.CreateSession(ctx, "sid", "g1", 20*time.Millisecond)
		require.NoError(t, err)
		clock.Advance(80 * time.Millisecond)

		ok, err := s.MatchSession(ctx, "sid", "g1")
		require.NoError(t, err)
		require.False(t, ok)
		ok, err = s.RotateSession(ctx, "sid", "g1", "g2", time.Minute)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("invalid ttl is rejected", func(t *testing.T) {
		s, _ := opts.New(t)
		_, err := s.CreateSession(ctx, "sid", "g1", 0)
		require.ErrorIs(t, err, store.ErrInvalidTTL)
		_, err = s.RotateSession(ctx, "sid", "g1", "g2", 0)
		require.ErrorIs(t, err, store.ErrTTLTooSmall)
	})

	t.Run("concurrent rotation has one winner", func(t *testing.T) {
		s, _ := opts.New(t)
		_, err := s.CreateSession(ctx, "sid", "g0", time.Minute)
		require.NoError(t, err)

		var wins, failures atomic.Int32
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := s.RotateSession(ctx, "sid", "g0", fmt.Sprintf("next-%d", i), time.Minute)
				if err != nil {
					failures.Add(1)
					return
				}
				if ok {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		require.Zero(t, failures.Load())
		require.EqualValues(t, 1, wins.Load())
	})
}
