package authn

import (
	"net/http"
	"strings"
)

// BearerToken returns the Bearer token from the Authorization header
// (RFC 9110 §11.1). Scheme matching is case-insensitive and surrounding
// whitespace around the token is ignored.
func BearerToken(r *http.Request) (string, bool) {
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
