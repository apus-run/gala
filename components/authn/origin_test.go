package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func originRequest(method, host, origin, secFetchSite string) *http.Request {
	r := httptest.NewRequest(method, "https://"+host+"/login", nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if secFetchSite != "" {
		r.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	return r
}

func TestOriginGuardCheck(t *testing.T) {
	guard, err := NewOriginGuard()
	require.NoError(t, err)

	tests := []struct {
		name    string
		r       *http.Request
		wantErr bool
	}{
		{name: "same origin", r: originRequest(http.MethodPost, "app.example.com", "https://app.example.com", "")},
		{name: "safe method is always allowed", r: originRequest(http.MethodGet, "app.example.com", "https://evil.example", "")},
		{name: "cross-origin post", r: originRequest(http.MethodPost, "app.example.com", "https://evil.example", ""), wantErr: true},
		{name: "cross-site fetch metadata", r: originRequest(http.MethodPost, "app.example.com", "", "cross-site"), wantErr: true},
		{name: "same-site fetch metadata from another host", r: originRequest(http.MethodPost, "app.example.com", "", "same-site"), wantErr: true},
		{name: "non-browser client without headers", r: originRequest(http.MethodPost, "app.example.com", "", "")},
		{name: "same-origin fetch metadata", r: originRequest(http.MethodPost, "app.example.com", "", "same-origin")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guard.Check(tt.r)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOriginGuardTrustedOrigin(t *testing.T) {
	guard, err := NewOriginGuard("https://app.example.com")
	require.NoError(t, err)

	// The SPA is served from another origin than the API, so it must be trusted explicitly.
	require.NoError(t, guard.Check(originRequest(http.MethodPost, "api.example.com", "https://app.example.com", "")))
	assert.Error(t, guard.Check(originRequest(http.MethodPost, "api.example.com", "https://evil.example", "")))
}

func TestOriginGuardRejectsInvalidOrigin(t *testing.T) {
	_, err := NewOriginGuard("https://app.example.com/path")
	assert.Error(t, err)
	_, err = NewOriginGuard("not-a-url")
	assert.Error(t, err)
}

func TestOriginGuardWrap(t *testing.T) {
	guard, err := NewOriginGuard()
	require.NoError(t, err)

	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	handler := guard.Wrap(next)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, originRequest(http.MethodPost, "app.example.com", "https://evil.example", ""))
	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, originRequest(http.MethodPost, "app.example.com", "https://app.example.com", ""))
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, w.Code)
}
