package authn

import (
	"net/http"
	"strings"
)

// TokenSource extracts a raw token from a request. ok is false when the request
// carries no token in this place; validating the token is not its job.
type TokenSource func(r *http.Request) (token string, ok bool)

// TokenFromHeader returns the Bearer token from the Authorization header
// (RFC 9110 §11.1). Scheme matching is case-insensitive and surrounding
// whitespace around the token is ignored. Another scheme, such as Basic, counts
// as no token.
func TokenFromHeader(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	return tok, tok != ""
}

// TokenFromCookie reads the token from the named cookie. Browsers attach cookies
// to cross-site requests too, so use it only behind CSRF protection.
func TokenFromCookie(name string) TokenSource {
	return func(r *http.Request) (string, bool) {
		if r == nil {
			return "", false
		}
		c, err := r.Cookie(name)
		if err != nil {
			return "", false
		}
		tok := strings.TrimSpace(c.Value)
		return tok, tok != ""
	}
}

// TokenFromQuery reads the token from the named query parameter. URLs end up in
// access logs, browser history and Referer headers, so reserve it for clients
// that cannot set headers, such as WebSocket handshakes or download links.
func TokenFromQuery(name string) TokenSource {
	return func(r *http.Request) (string, bool) {
		if r == nil || r.URL == nil {
			return "", false
		}
		tok := strings.TrimSpace(r.URL.Query().Get(name))
		return tok, tok != ""
	}
}

// firstToken returns the token of the first source that has one.
func firstToken(r *http.Request, sources []TokenSource) (string, bool) {
	for _, src := range sources {
		if tok, ok := src(r); ok {
			return tok, true
		}
	}
	return "", false
}
