package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderCarrier(t *testing.T) {
	pair := &TokenPair{AccessToken: "a", RefreshToken: "r"}

	t.Run("zero value defaults", func(t *testing.T) {
		var c HeaderCarrier
		w := httptest.NewRecorder()
		c.Inject(w, pair)
		assert.Equal(t, "a", w.Header().Get("X-Access-Token"))
		assert.Equal(t, "r", w.Header().Get("X-Refresh-Token"))

		c.Clear(w)
		assert.Empty(t, w.Header().Get("X-Access-Token"))
		assert.Empty(t, w.Header().Get("X-Refresh-Token"))
	})

	t.Run("custom names", func(t *testing.T) {
		c := &HeaderCarrier{AccessName: "X-Token", RefreshName: "X-Refresh"}
		w := httptest.NewRecorder()
		c.Inject(w, pair)
		assert.Equal(t, "a", w.Header().Get("X-Token"))
		assert.Equal(t, "r", w.Header().Get("X-Refresh"))

		c.Clear(w)
		assert.Empty(t, w.Header().Get("X-Token"))
		assert.Empty(t, w.Header().Get("X-Refresh"))
	})

	t.Run("nil pair writes nothing", func(t *testing.T) {
		var c HeaderCarrier
		w := httptest.NewRecorder()
		c.Inject(w, nil)
		assert.Empty(t, w.Header())
	})
}

func TestHeaderCarrierRoundTripWithAuthFunc(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)

	var carrier HeaderCarrier
	w := httptest.NewRecorder()
	carrier.Inject(w, pair)

	// A client echoes both response headers back unchanged; the middleware then
	// authenticates from the access header without any Bearer prefix.
	// 客户端把两个响应头原样回传，中间件从访问令牌头完成认证，无需 Bearer 前缀。
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for name, values := range w.Header() {
		r.Header.Set(name, values[0])
	}
	claims, err := auth.AuthFunc(carrier.AccessSource())(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "42", claims.Subject)

	raw, ok := carrier.RefreshSource()(r)
	require.True(t, ok)
	assert.Equal(t, pair.RefreshToken, raw)

	next, err := auth.Refresh(ctx, raw)
	require.NoError(t, err)
	assert.NotEmpty(t, next.AccessToken)
}

func TestTokenFromNamedHeader(t *testing.T) {
	src := TokenFromNamedHeader("X-Access-Token")
	tests := []struct {
		name, header, want string
		ok                 bool
	}{
		{name: "present", header: "tok", want: "tok", ok: true},
		{name: "surrounding whitespace", header: "  tok  ", want: "tok", ok: true},
		{name: "missing"},
		{name: "empty", header: "   "},
		// The Bearer scheme is not parsed; the whole value is the token.
		// 不解析 Bearer 前缀，整段就是令牌。
		{name: "bearer scheme is not stripped", header: "Bearer tok", want: "Bearer tok", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("X-Access-Token", tt.header)
			}
			got, ok := src(r)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
	_, ok := src(nil)
	assert.False(t, ok)
}

func TestCookieCarrier(t *testing.T) {
	pair := &TokenPair{AccessToken: "a", RefreshToken: "r"}

	t.Run("zero value defaults", func(t *testing.T) {
		var c CookieCarrier
		w := httptest.NewRecorder()
		c.Inject(w, pair)
		byName := cookiesByName(w.Result().Cookies())
		require.Contains(t, byName, "access_token")
		require.Contains(t, byName, "refresh_token")

		access := byName["access_token"]
		assert.Equal(t, "a", access.Value)
		assert.Equal(t, "/", access.Path)
		assert.True(t, access.HttpOnly)
		assert.False(t, access.Secure)
		assert.Zero(t, access.MaxAge)
		assert.Equal(t, "r", byName["refresh_token"].Value)

		xsrf := byName[token.XSRFCookieName]
		require.NotNil(t, xsrf)
		assert.NotEmpty(t, xsrf.Value)
		assert.False(t, xsrf.HttpOnly, "JS must be able to read the XSRF cookie")

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, cookie := range w.Result().Cookies() {
			r.AddCookie(cookie)
		}
		got, ok := c.AccessSource()(r)
		assert.True(t, ok)
		assert.Equal(t, "a", got)
		got, ok = c.RefreshSource()(r)
		assert.True(t, ok)
		assert.Equal(t, "r", got)
	})

	t.Run("custom names and attributes", func(t *testing.T) {
		c := &CookieCarrier{
			AccessName: "at", RefreshName: "rt",
			Path: "/app", Domain: "example.com", Secure: true,
			SameSite: http.SameSiteStrictMode, MaxAge: 3600,
		}
		w := httptest.NewRecorder()
		c.Inject(w, pair)
		byName := cookiesByName(w.Result().Cookies())
		require.Len(t, byName, 3)
		assert.Equal(t, "a", byName["at"].Value)
		assert.Equal(t, "r", byName["rt"].Value)
		for name, cookie := range byName {
			assert.Equal(t, "/app", cookie.Path)
			assert.Equal(t, "example.com", cookie.Domain)
			assert.True(t, cookie.Secure)
			assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
			assert.Equal(t, 3600, cookie.MaxAge)
			assert.Equal(t, name != token.XSRFCookieName, cookie.HttpOnly)
		}
	})

	t.Run("SameSite None forces Secure", func(t *testing.T) {
		c := &CookieCarrier{SameSite: http.SameSiteNoneMode}
		w := httptest.NewRecorder()
		c.Inject(w, pair)
		for _, cookie := range w.Result().Cookies() {
			assert.True(t, cookie.Secure)
		}
	})

	t.Run("cookie prefixes are enforced", func(t *testing.T) {
		c := &CookieCarrier{
			AccessName: "__Host-at", RefreshName: "__Secure-rt",
			Path: "/app", Domain: "example.com",
		}
		w := httptest.NewRecorder()
		c.Inject(w, pair)
		byName := cookiesByName(w.Result().Cookies())

		host := byName["__Host-at"]
		require.NotNil(t, host)
		assert.True(t, host.Secure)
		assert.Equal(t, "/", host.Path)
		assert.Empty(t, host.Domain)
		assert.True(t, host.HttpOnly)

		secure := byName["__Secure-rt"]
		require.NotNil(t, secure)
		assert.True(t, secure.Secure)
		assert.Equal(t, "/app", secure.Path)
		assert.Equal(t, "example.com", secure.Domain)

		// The XSRF cookie keeps its fixed name and the configured attributes.
		xsrf := byName[token.XSRFCookieName]
		assert.False(t, xsrf.Secure)
		assert.Equal(t, "/app", xsrf.Path)

		// Clearing must use the same enforced attributes, or the browser keeps the cookie.
		cleared := httptest.NewRecorder()
		c.Clear(cleared)
		hostClear := cookiesByName(cleared.Result().Cookies())["__Host-at"]
		require.NotNil(t, hostClear)
		assert.True(t, hostClear.Secure)
		assert.Equal(t, "/", hostClear.Path)
		assert.Empty(t, hostClear.Domain)
		assert.Less(t, hostClear.MaxAge, 0)
	})

	t.Run("clear expires cookies with matching attributes", func(t *testing.T) {
		c := &CookieCarrier{Path: "/app", Domain: "example.com"}
		w := httptest.NewRecorder()
		c.Clear(w)
		cookies := w.Result().Cookies()
		require.Len(t, cookies, 3)
		for _, cookie := range cookies {
			assert.Empty(t, cookie.Value)
			assert.Less(t, cookie.MaxAge, 0)
			assert.True(t, cookie.Expires.Before(time.Now()))
			assert.Equal(t, "/app", cookie.Path)
			assert.Equal(t, "example.com", cookie.Domain)
			assert.Equal(t, cookie.Name != token.XSRFCookieName, cookie.HttpOnly)
		}
	})

	t.Run("nil pair writes nothing", func(t *testing.T) {
		var c CookieCarrier
		w := httptest.NewRecorder()
		c.Inject(w, nil)
		assert.Empty(t, w.Result().Cookies())
	})
}

func TestCookieCarrierConcurrentDelivery(t *testing.T) {
	c := &CookieCarrier{AccessName: "at", RefreshName: "rt"}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pair := &TokenPair{AccessToken: fmt.Sprintf("a-%d", i), RefreshToken: fmt.Sprintf("r-%d", i)}
			w := httptest.NewRecorder()
			c.Inject(w, pair)

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, cookie := range w.Result().Cookies() {
				r.AddCookie(cookie)
			}
			got, ok := c.AccessSource()(r)
			if !ok || got != pair.AccessToken {
				t.Errorf("access mismatch: got %q want %q", got, pair.AccessToken)
			}
			w2 := httptest.NewRecorder()
			c.Inject(w2, pair)
			// Each delivery mints its own XSRF value; sharing one would let a
			// page from another tab replay it.
			// 每次交付都生成自己的 XSRF 值；共用会让另一个标签页的页面能重放。
			r2 := httptest.NewRequest(http.MethodPost, "/", nil)
			for _, cookie := range w2.Result().Cookies() {
				r2.AddCookie(cookie)
			}
			r2.Header.Set(token.XSRFHeaderKey, cookieValue(w2.Result().Cookies(), token.XSRFCookieName))
			if err := c.VerifyCSRF(r2); err != nil {
				t.Errorf("verify with own delivery: %v", err)
			}
			if err := c.VerifyCSRF(r2); err != nil {
				t.Errorf("verify is not idempotent: %v", err)
			}
		}()
	}
	wg.Wait()
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func BenchmarkCookieCarrierVerifyCSRF(b *testing.B) {
	var c CookieCarrier
	w := httptest.NewRecorder()
	c.Inject(w, &TokenPair{AccessToken: "a", RefreshToken: "r"})
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	r.Header.Set(token.XSRFHeaderKey, cookieValue(w.Result().Cookies(), token.XSRFCookieName))
	b.ReportAllocs()
	for b.Loop() {
		if err := c.VerifyCSRF(r); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCookieCarrierRoundTripWithAuthFunc(t *testing.T) {
	auth, _ := newSessionTest(t)
	ctx := context.Background()
	pair, err := auth.Issue(ctx, "42")
	require.NoError(t, err)

	var carrier CookieCarrier
	w := httptest.NewRecorder()
	carrier.Inject(w, pair)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range w.Result().Cookies() {
		r.AddCookie(cookie)
	}
	claims, err := auth.AuthFunc(carrier.AccessSource())(ctx, r)
	require.NoError(t, err)
	assert.Equal(t, "42", claims.Subject)
}

func TestCookieCarrierVerifyCSRF(t *testing.T) {
	var c CookieCarrier
	w := httptest.NewRecorder()
	c.Inject(w, &TokenPair{AccessToken: "a", RefreshToken: "r"})
	byName := cookiesByName(w.Result().Cookies())
	xsrf := byName[token.XSRFCookieName].Value
	require.NotEmpty(t, xsrf)

	withCookies := func(method string, xsrfHeader string) *http.Request {
		r := httptest.NewRequest(method, "/", nil)
		for _, cookie := range w.Result().Cookies() {
			r.AddCookie(cookie)
		}
		if xsrfHeader != "" {
			r.Header.Set(token.XSRFHeaderKey, xsrfHeader)
		}
		return r
	}

	tests := []struct {
		name    string
		r       *http.Request
		wantErr bool
	}{
		{name: "safe method without header", r: withCookies(http.MethodGet, "")},
		{name: "post with matching header", r: withCookies(http.MethodPost, xsrf)},
		{name: "post without header", r: withCookies(http.MethodPost, ""), wantErr: true},
		{name: "post with wrong header", r: withCookies(http.MethodPost, "wrong"), wantErr: true},
		{name: "delete is checked too", r: withCookies(http.MethodDelete, ""), wantErr: true},
		{name: "header-only client without cookies", r: func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Header.Set("Authorization", "Bearer a")
			return r
		}()},
		// The refresh endpoint carries only the refresh cookie; it must still
		// present the double-submit pair.
		// 刷新接口只带 refresh cookie，同样必须提供双提交对。
		{name: "refresh cookie with xsrf pair", r: func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.AddCookie(byName["refresh_token"])
			r.AddCookie(byName[token.XSRFCookieName])
			r.Header.Set(token.XSRFHeaderKey, xsrf)
			return r
		}()},
		{name: "refresh cookie without xsrf cookie", r: func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.AddCookie(byName["refresh_token"])
			r.Header.Set(token.XSRFHeaderKey, xsrf)
			return r
		}(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.VerifyCSRF(tt.r)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrCSRFMismatch)
			} else {
				assert.NoError(t, err)
			}
		})
	}
	t.Run("duplicate XSRF cookies are rejected", func(t *testing.T) {
		r := withCookies(http.MethodPost, xsrf)
		r.AddCookie(&http.Cookie{Name: token.XSRFCookieName, Value: xsrf})
		assert.ErrorIs(t, c.VerifyCSRF(r), ErrCSRFMismatch)
	})

	assert.NoError(t, c.VerifyCSRF(nil))

	t.Run("xsrf cookie rotated on every delivery", func(t *testing.T) {
		other := httptest.NewRecorder()
		c.Inject(other, &TokenPair{AccessToken: "a", RefreshToken: "r"})
		assert.NotEqual(t, xsrf, cookiesByName(other.Result().Cookies())[token.XSRFCookieName].Value)
	})
}

func TestCookieCarrierProtect(t *testing.T) {
	var c CookieCarrier
	innerCalls := 0
	inner := func(_ context.Context, r *http.Request) (*SessionClaims, error) {
		innerCalls++
		raw, ok := c.AccessSource()(r)
		if !ok {
			return nil, ErrMissingToken
		}
		return &SessionClaims{SessionID: raw}, nil
	}
	protected := c.Protect(inner)

	// Unauthenticated: 401 wins over 403.
	_, err := protected(context.Background(), httptest.NewRequest(http.MethodPost, "/", nil))
	assert.ErrorIs(t, err, ErrMissingToken)

	issued := httptest.NewRecorder()
	c.Inject(issued, &TokenPair{AccessToken: "a", RefreshToken: "r"})
	xsrf := cookiesByName(issued.Result().Cookies())[token.XSRFCookieName].Value

	post := func(xsrfHeader string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		for _, cookie := range issued.Result().Cookies() {
			r.AddCookie(cookie)
		}
		if xsrfHeader != "" {
			r.Header.Set(token.XSRFHeaderKey, xsrfHeader)
		}
		return r
	}
	_, err = protected(context.Background(), post(""))
	assert.ErrorIs(t, err, ErrCSRFMismatch)

	claims, err := protected(context.Background(), post(xsrf))
	require.NoError(t, err)
	assert.Equal(t, "a", claims.SessionID)
	assert.Equal(t, 3, innerCalls, "inner auth runs before the CSRF check, not instead of it")

	code, challenge := StatusOf(ErrCSRFMismatch)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Empty(t, challenge)
}

func cookiesByName(cookies []*http.Cookie) map[string]*http.Cookie {
	byName := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	return byName
}
