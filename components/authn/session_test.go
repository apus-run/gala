package authn

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/token"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type sessionEntry struct {
	generation string
	expires    time.Time
}
type sessionMemory struct {
	sync.Mutex
	entries map[string]sessionEntry
	failure error
}

func (m *sessionMemory) CreateSession(_ context.Context, id, g string, ttl time.Duration) (bool, error) {
	m.Lock()
	defer m.Unlock()
	if m.failure != nil {
		return false, m.failure
	}
	if e, ok := m.entries[id]; ok && time.Now().Before(e.expires) {
		return false, nil
	}
	m.entries[id] = sessionEntry{g, time.Now().Add(ttl)}
	return true, nil
}
func (m *sessionMemory) MatchSession(_ context.Context, id, g string) (bool, error) {
	m.Lock()
	defer m.Unlock()
	e := m.entries[id]
	return e.generation == g && time.Now().Before(e.expires), m.failure
}
func (m *sessionMemory) RotateSession(_ context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	m.Lock()
	defer m.Unlock()
	if m.failure != nil {
		return false, m.failure
	}
	e := m.entries[id]
	if e.generation != old || !time.Now().Before(e.expires) {
		return false, nil
	}
	m.entries[id] = sessionEntry{next, time.Now().Add(ttl)}
	return true, nil
}
func (m *sessionMemory) DeleteSession(_ context.Context, id string) error {
	m.Lock()
	defer m.Unlock()
	if m.failure != nil {
		return m.failure
	}
	delete(m.entries, id)
	return nil
}
func newSessionTest(t *testing.T) (*SessionAuth, *sessionMemory) {
	t.Helper()
	m := &sessionMemory{entries: map[string]sessionEntry{}}
	s, err := NewSessionAuth(m, WithJWTOptions(token.WithSecretKey(testSecret)))
	require.NoError(t, err)
	return s, m
}
func TestSessionLifecycle(t *testing.T) {
	s, _ := newSessionTest(t)
	ctx := context.Background()
	first, err := s.Issue(ctx, "42")
	require.NoError(t, err)
	other, err := s.Issue(ctx, "42")
	require.NoError(t, err)
	claims, err := s.ParseAccess(ctx, first.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "42", claims.Subject)
	require.Equal(t, 15*time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
	refreshClaims, err := s.parse(first.RefreshToken, "refresh")
	require.NoError(t, err)
	require.Equal(t, 7*24*time.Hour, refreshClaims.ExpiresAt.Sub(refreshClaims.IssuedAt.Time))
	_, err = s.ParseAccess(ctx, first.RefreshToken)
	require.Error(t, err)
	_, err = s.Refresh(ctx, first.AccessToken)
	require.Error(t, err)
	next, err := s.Refresh(ctx, first.RefreshToken)
	require.NoError(t, err)
	_, err = s.ParseAccess(ctx, first.AccessToken)
	require.ErrorIs(t, err, ErrSessionInvalid)
	_, err = s.Refresh(ctx, first.RefreshToken)
	require.ErrorIs(t, err, ErrSessionInvalid)
	_, err = s.ParseAccess(ctx, next.AccessToken)
	require.NoError(t, err)
	require.NoError(t, s.Logout(ctx, first.AccessToken)) // old generation logs out the whole session
	require.NoError(t, s.Logout(ctx, next.RefreshToken))
	_, err = s.ParseAccess(ctx, next.AccessToken)
	require.ErrorIs(t, err, ErrSessionInvalid)
	_, err = s.Refresh(ctx, next.RefreshToken)
	require.ErrorIs(t, err, ErrSessionInvalid)
	_, err = s.ParseAccess(ctx, other.AccessToken)
	require.NoError(t, err)
}
func TestSessionConcurrentRefresh(t *testing.T) {
	s, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := s.Issue(ctx, "42")
	require.NoError(t, err)
	var wg sync.WaitGroup
	var winners atomic.Int32
	for range 32 {
		wg.Go(func() {
			if _, err := s.Refresh(ctx, pair.RefreshToken); err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrSessionInvalid) {
				t.Errorf("refresh: %v", err)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, winners.Load())
}

// operationBarrier pauses the first operation immediately before its storage call.
// Context cancellation also releases it, so failed assertions cannot leak workers.
type operationBarrier struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newOperationBarrier() *operationBarrier {
	return &operationBarrier{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *operationBarrier) wait(ctx context.Context) error {
	b.once.Do(func() {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
	return ctx.Err()
}

type gatedSessionStore struct {
	store.SessionStorer
	rotate, delete *operationBarrier
}

func (s *gatedSessionStore) RotateSession(ctx context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	if err := s.rotate.wait(ctx); err != nil {
		return false, err
	}
	return s.SessionStorer.RotateSession(ctx, id, old, next, ttl)
}

func (s *gatedSessionStore) DeleteSession(ctx context.Context, id string) error {
	if err := s.delete.wait(ctx); err != nil {
		return err
	}
	return s.SessionStorer.DeleteSession(ctx, id)
}

func awaitSessionResult[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-ctx.Done():
		t.Fatalf("timed out waiting for session operation: %v", ctx.Err())
		var zero T
		return zero
	}
}

func TestSessionLogoutRacesRefresh(t *testing.T) {
	backends := map[string]func(*testing.T) store.SessionStorer{
		"memory": func(*testing.T) store.SessionStorer {
			return &sessionMemory{entries: map[string]sessionEntry{}}
		},
		"redis": func(t *testing.T) store.SessionStorer {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			return store.NewStore(client, "auth:race:")
		},
	}
	for backend, newStore := range backends {
		t.Run(backend, func(t *testing.T) {
			for _, refreshFirst := range []bool{true, false} {
				name := "logout_before_refresh"
				if refreshFirst {
					name = "refresh_before_logout"
				}
				t.Run(name, func(t *testing.T) {
					base := newStore(t)
					gated := &gatedSessionStore{SessionStorer: base, rotate: newOperationBarrier(), delete: newOperationBarrier()}
					auth, err := NewSessionAuth(gated, WithJWTOptions(token.WithSecretKey(testSecret)))
					require.NoError(t, err)
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					pair, err := auth.Issue(ctx, "42")
					require.NoError(t, err)
					original, err := auth.ParseAccess(ctx, pair.AccessToken)
					require.NoError(t, err)
					type refreshResult struct {
						pair *TokenPair
						err  error
					}
					refreshed := make(chan refreshResult, 1)
					loggedOut := make(chan error, 1)
					var workers sync.WaitGroup
					defer func() { cancel(); workers.Wait() }()
					workers.Go(func() {
						next, err := auth.Refresh(ctx, pair.RefreshToken)
						refreshed <- refreshResult{next, err}
					})
					workers.Go(func() { loggedOut <- auth.Logout(ctx, pair.AccessToken) })

					// Both calls have validated their JWTs and reached the storage boundary.
					awaitSessionResult(t, ctx, gated.rotate.entered)
					awaitSessionResult(t, ctx, gated.delete.entered)
					var result refreshResult
					if refreshFirst {
						close(gated.rotate.release)
						result = awaitSessionResult(t, ctx, refreshed)
						require.NoError(t, result.err)
						require.NotNil(t, result.pair)
						_, err = auth.ParseAccess(ctx, result.pair.AccessToken)
						require.NoError(t, err, "winning generation must work before logout")
						close(gated.delete.release)
						require.NoError(t, awaitSessionResult(t, ctx, loggedOut))
					} else {
						close(gated.delete.release)
						require.NoError(t, awaitSessionResult(t, ctx, loggedOut))
						close(gated.rotate.release)
						result = awaitSessionResult(t, ctx, refreshed)
						require.ErrorIs(t, result.err, ErrSessionInvalid)
						require.Nil(t, result.pair, "failed refresh must not deliver tokens")
					}
					workers.Wait()
					// Always assert final invalidation, including when refresh returned no pair.
					_, err = auth.ParseAccess(ctx, pair.AccessToken)
					require.ErrorIs(t, err, ErrSessionInvalid)
					_, err = auth.Refresh(ctx, pair.RefreshToken)
					require.ErrorIs(t, err, ErrSessionInvalid)
					if refreshFirst {
						_, err = auth.ParseAccess(ctx, result.pair.AccessToken)
						require.ErrorIs(t, err, ErrSessionInvalid)
						_, err = auth.Refresh(ctx, result.pair.RefreshToken)
						require.ErrorIs(t, err, ErrSessionInvalid)
					}
					// Missing session, rather than just an unmatched generation: it cannot be
					// rotated again, and the same key can be created only as a fresh session.
					created, err := base.CreateSession(ctx, original.SessionID, "probe", time.Minute)
					require.NoError(t, err)
					require.True(t, created, "logout must delete the session key")
				})
			}
		})
	}
}
func TestSessionStorageFailureAndExpiry(t *testing.T) {
	s, m := newSessionTest(t)
	ctx := context.Background()
	pair, err := s.Issue(ctx, "42")
	require.NoError(t, err)
	failure := errors.New("storage unavailable")
	m.failure = failure
	_, err = s.Issue(ctx, "42")
	require.ErrorIs(t, err, failure)
	_, err = s.ParseAccess(ctx, pair.AccessToken)
	require.ErrorIs(t, err, failure)
	_, err = s.Refresh(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, s.Logout(ctx, pair.AccessToken), failure)
	m.failure = nil
	for id, e := range m.entries {
		e.expires = time.Now().Add(-time.Second)
		m.entries[id] = e
	}
	_, err = s.ParseAccess(ctx, pair.AccessToken)
	require.ErrorIs(t, err, ErrSessionInvalid)
	_, err = s.Refresh(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, ErrSessionInvalid)
}
func TestSessionConfigurationAndInvalidTokens(t *testing.T) {
	s, m := newSessionTest(t)
	ctx := context.Background()
	_, err := NewSessionAuth(nil)
	require.ErrorIs(t, err, ErrEmptySessionStore)
	_, err = NewSessionAuth(m, WithSessionTTL(time.Minute, time.Second))
	require.Error(t, err)
	_, err = s.Issue(ctx, "")
	require.Error(t, err)
	_, err = s.ParseAccess(ctx, "garbage")
	require.Error(t, err)
	require.Error(t, s.Logout(ctx, "garbage"))
	pair, err := s.Issue(ctx, "42")
	require.NoError(t, err)
	c, err := s.parse(pair.AccessToken, "access")
	require.NoError(t, err)
	c.ExpiresAt.Time = time.Now().Add(-time.Hour)
	expired, err := s.manager.GenerateClaims(c)
	require.NoError(t, err)
	_, err = s.ParseAccess(ctx, expired)
	require.Error(t, err)
	require.Error(t, s.Logout(ctx, expired))
	c.ExpiresAt.Time = time.Now().Add(time.Hour)
	c.SessionID = ""
	malformed, err := s.manager.GenerateClaims(c)
	require.NoError(t, err)
	_, err = s.ParseAccess(ctx, malformed)
	require.ErrorIs(t, err, ErrTokenInvalid)
}
