package authn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenFromCookie(t *testing.T) {
	src := TokenFromCookie("access")
	tests := []struct {
		name   string
		cookie *http.Cookie
		want   string
		ok     bool
	}{
		{name: "present", cookie: &http.Cookie{Name: "access", Value: "x"}, want: "x", ok: true},
		{name: "missing"},
		{name: "empty value", cookie: &http.Cookie{Name: "access", Value: ""}},
		{name: "other cookie", cookie: &http.Cookie{Name: "session", Value: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}
			got, ok := src(r)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
	_, ok := src(nil)
	assert.False(t, ok)
}

func TestTokenFromQuery(t *testing.T) {
	src := TokenFromQuery("token")
	tests := []struct {
		name, url, want string
		ok              bool
	}{
		{name: "present", url: "/ws?token=x", want: "x", ok: true},
		{name: "surrounding whitespace", url: "/ws?token=%20x%20", want: "x", ok: true},
		{name: "missing", url: "/ws"},
		{name: "empty", url: "/ws?token="},
		{name: "other param", url: "/ws?access=x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := src(httptest.NewRequest(http.MethodGet, tt.url, nil))
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
	_, ok := src(nil)
	assert.False(t, ok)
}

func TestAuthFuncDefaultsToHeaderOnly(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)

	// Without sources a cookie or query token must not authenticate.
	r := httptest.NewRequest(http.MethodGet, "/?token="+pair.AccessToken, nil)
	r.AddCookie(&http.Cookie{Name: "access", Value: pair.AccessToken})
	_, err = auth.AuthFunc()(ctx, r)
	assert.ErrorIs(t, err, ErrMissingToken)

	r.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	claims, err := auth.AuthFunc()(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "42", claims.Subject)
}

func TestAuthFuncSourceOrder(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	alice, err := auth.Issue(ctx, "alice")
	require.NoError(t, err)
	bob, err := auth.Issue(ctx, "bob")
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+alice.AccessToken)
	r.AddCookie(&http.Cookie{Name: "access", Value: bob.AccessToken})

	// The first source that has a token wins.
	claims, err := auth.AuthFunc(TokenFromHeader, TokenFromCookie("access"))(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "alice", claims.Subject)

	claims, err = auth.AuthFunc(TokenFromCookie("access"), TokenFromHeader)(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "bob", claims.Subject)
}

func TestAuthFuncFallsThroughEmptySources(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)
	authFn := auth.AuthFunc(TokenFromHeader, TokenFromCookie("access"), TokenFromQuery("token"))

	cases := map[string]*http.Request{
		"cookie": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.AddCookie(&http.Cookie{Name: "access", Value: pair.AccessToken})
			return r
		}(),
		"query": httptest.NewRequest(http.MethodGet, "/?token="+pair.AccessToken, nil),
		// A non-Bearer Authorization header is no token, so the chain continues.
		"cookie after Basic header": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
			r.AddCookie(&http.Cookie{Name: "access", Value: pair.AccessToken})
			return r
		}(),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			claims, err := authFn(ctx, r)
			require.NoError(t, err)
			assert.Equal(t, "42", claims.Subject)
		})
	}

	_, err = authFn(ctx, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.ErrorIs(t, err, ErrMissingToken)
}

func TestAuthFuncInvalidTokenDoesNotFallBack(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)

	// A presented but invalid header token must fail, never fall back to the cookie.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer garbage")
	r.AddCookie(&http.Cookie{Name: "access", Value: pair.AccessToken})

	_, err = auth.AuthFunc(TokenFromHeader, TokenFromCookie("access"))(ctx, r)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestAuthFuncCustomSource(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)

	// Any func(*http.Request) (string, bool) is a TokenSource.
	fromHeader := func(r *http.Request) (string, bool) {
		tok := r.Header.Get("X-Access-Token")
		return tok, tok != ""
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Access-Token", pair.AccessToken)

	claims, err := auth.AuthFunc(fromHeader)(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "42", claims.Subject)
}

func TestAuthFuncCopiesSources(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)

	sources := []TokenSource{TokenFromCookie("access")}
	authFn := auth.AuthFunc(sources...)
	// Mutating the caller's slice afterwards must not change the AuthFunc.
	sources[0] = TokenFromQuery("token")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "access", Value: pair.AccessToken})
	claims, err := authFn(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "42", claims.Subject)
}

func TestAuthFuncSessionChecksApplyToEverySource(t *testing.T) {
	auth, store := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)
	authFn := auth.AuthFunc(TokenFromCookie("access"))
	withCookie := func(tok string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "access", Value: tok})
		return r
	}

	_, err = authFn(ctx, withCookie(pair.RefreshToken))
	assert.ErrorIs(t, err, ErrTokenInvalid, "refresh tokens must not authenticate from a cookie either")

	storeDown := errors.New("redis down")
	store.failure = storeDown
	_, err = authFn(ctx, withCookie(pair.AccessToken))
	assert.ErrorIs(t, err, storeDown)
	store.failure = nil

	require.NoError(t, auth.Logout(ctx, pair.AccessToken))
	_, err = authFn(ctx, withCookie(pair.AccessToken))
	assert.ErrorIs(t, err, ErrSessionInvalid)
}
