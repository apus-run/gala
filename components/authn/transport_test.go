package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{name: "standard", header: "Bearer x", want: "x", ok: true},
		{name: "lowercase scheme", header: "bearer x", want: "x", ok: true},
		{name: "uppercase scheme", header: "BEARER x", want: "x", ok: true},
		{name: "extra token whitespace", header: "Bearer    x   ", want: "x", ok: true},
		{name: "missing header"},
		{name: "missing token", header: "Bearer"},
		{name: "only whitespace", header: "Bearer    "},
		{name: "wrong scheme", header: "Basic x"},
		{name: "no separator", header: "Bearerx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			got, ok := BearerToken(r)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
	assert.False(t, func() bool {
		_, ok := BearerToken(nil)
		return ok
	}())
}

func TestContextIdentity(t *testing.T) {
	want := &SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "42"}}
	ctx := NewContext(context.Background(), want)
	got, ok := FromContext[*SessionClaims](ctx)
	require.True(t, ok)
	assert.Same(t, want, got)

	_, ok = FromContext[string](ctx)
	assert.False(t, ok, "wrong type must not panic or match")

	_, ok = FromContext[*SessionClaims](context.Background())
	assert.False(t, ok)

	_, ok = FromContext[*SessionClaims](nil)
	assert.False(t, ok)
}

func TestStatusOf(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	tests := []struct {
		name      string
		err       error
		code      int
		challenge string
	}{
		{name: "missing token", err: ErrMissingToken, code: http.StatusUnauthorized, challenge: "Bearer"},
		{name: "wrapped missing token", err: fmt.Errorf("wrapped: %w", ErrMissingToken), code: http.StatusUnauthorized, challenge: "Bearer"},
		{name: "invalid token", err: ErrTokenInvalid, code: http.StatusUnauthorized, challenge: `Bearer error="invalid_token"`},
		{name: "invalid token wrapped", err: fmt.Errorf("wrapped: %w", ErrTokenInvalid), code: http.StatusUnauthorized, challenge: `Bearer error="invalid_token"`},
		{name: "invalid session", err: ErrSessionInvalid, code: http.StatusUnauthorized, challenge: `Bearer error="invalid_token"`},
		{name: "storage failure", err: storageErr, code: http.StatusServiceUnavailable},
		{name: "wrapped storage failure", err: fmt.Errorf("wrapped: %w", storageErr), code: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, challenge := StatusOf(tt.err)
			assert.Equal(t, tt.code, code)
			assert.Equal(t, tt.challenge, challenge)
		})
	}
}

func TestSessionAuthRejectsEmptyToken(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()

	for _, raw := range []string{"", "   ", "\t"} {
		_, err := auth.ParseAccess(ctx, raw)
		require.ErrorIs(t, err, ErrTokenInvalid)
		code, challenge := StatusOf(err)
		assert.Equal(t, http.StatusUnauthorized, code)
		assert.Equal(t, `Bearer error="invalid_token"`, challenge)
	}

	_, err := auth.Refresh(ctx, "")
	assert.ErrorIs(t, err, ErrTokenInvalid)
	assert.ErrorIs(t, auth.Logout(ctx, "   "), ErrTokenInvalid)
}

func TestSessionAuthFunc(t *testing.T) {
	auth, store := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)
	authFn := auth.AuthFunc()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err = authFn(ctx, r)
	assert.ErrorIs(t, err, ErrMissingToken)

	r.Header.Set("Authorization", "Basic x")
	_, err = authFn(ctx, r)
	assert.ErrorIs(t, err, ErrMissingToken)

	r.Header.Set("Authorization", "Bearer garbage")
	_, err = authFn(ctx, r)
	assert.ErrorIs(t, err, ErrTokenInvalid, "JWT parse errors must remain recognizable by StatusOf")

	r.Header.Set("Authorization", "Bearer "+pair.RefreshToken)
	_, err = authFn(ctx, r)
	assert.ErrorIs(t, err, ErrTokenInvalid, "refresh tokens must not authenticate as access tokens")

	r.Header.Set("Authorization", "bearer   "+pair.AccessToken+"  ")
	claims, err := authFn(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "42", claims.Subject)

	require.NoError(t, auth.Logout(ctx, pair.AccessToken))
	_, err = authFn(ctx, r)
	assert.ErrorIs(t, err, ErrSessionInvalid)

	otherPair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)
	storageFailure := errors.New("redis down")
	store.failure = storageFailure
	r.Header.Set("Authorization", "Bearer "+otherPair.AccessToken)
	_, err = authFn(ctx, r)
	assert.ErrorIs(t, err, storageFailure)
}

func TestMiddleware(t *testing.T) {
	authFn := func(_ context.Context, r *http.Request) (*SessionClaims, error) {
		token, ok := BearerToken(r)
		if !ok {
			return nil, ErrMissingToken
		}
		if token != "good" {
			return nil, ErrTokenInvalid
		}
		return &SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: r.Header.Get("X-User")}}, nil
	}

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		claims, ok := FromContext[*SessionClaims](r.Context())
		if !ok {
			t.Fatal("identity missing from request context")
		}
		w.Header().Set("X-Subject", claims.Subject)
		w.WriteHeader(http.StatusNoContent)
	})
	mw := NewMiddleware(authFn).Wrap(next)

	t.Run("missing token", func(t *testing.T) {
		nextCalled = false
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.False(t, nextCalled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, "Bearer", w.Header().Get("WWW-Authenticate"))
	})

	t.Run("invalid token", func(t *testing.T) {
		nextCalled = false
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer bad")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		assert.False(t, nextCalled)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, `Bearer error="invalid_token"`, w.Header().Get("WWW-Authenticate"))
	})

	t.Run("success", func(t *testing.T) {
		nextCalled = false
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer good")
		r.Header.Set("X-User", "42")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, r)
		assert.True(t, nextCalled)
		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "42", w.Header().Get("X-Subject"))
	})
}

func TestMiddlewareCustomErrorHandler(t *testing.T) {
	authFn := func(context.Context, *http.Request) (string, error) {
		return "", errors.New("storage down")
	}
	called := false
	mw := NewMiddleware(authFn, WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
		called = true
		http.Error(w, err.Error(), http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	mw.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not be called")
	})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called)
	assert.Equal(t, http.StatusTeapot, w.Code)
}

func TestMiddlewareConcurrentIdentities(t *testing.T) {
	authFn := func(_ context.Context, r *http.Request) (string, error) {
		token, ok := BearerToken(r)
		if !ok {
			return "", ErrMissingToken
		}
		return token, nil
	}
	mw := NewMiddleware(authFn).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, ok := FromContext[string](r.Context())
		if !ok {
			t.Error("identity missing from request context")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(subject))
	}))

	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subject := fmt.Sprintf("user-%d", i)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "Bearer "+subject)
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Body.String() != subject {
				t.Errorf("request %s got status %d body %q", subject, w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
}

func TestNewSessionAuthRejectsWithClaims(t *testing.T) {
	s := &sessionMemory{entries: map[string]sessionEntry{}}
	_, err := NewSessionAuth(s, WithClaims(func() jwt.Claims { return &SessionClaims{} }))
	assert.ErrorIs(t, err, ErrSessionClaimsUnsupported)
}
