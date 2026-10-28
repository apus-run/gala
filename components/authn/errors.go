package authn

import (
	"errors"
	"net/http"
)

// ErrMissingToken indicates that the request did not contain a usable Bearer
// token. It is deliberately distinct from token validation failures.
var ErrMissingToken = errors.New("authn: missing bearer token")

// ErrSessionClaimsUnsupported is returned when WithClaims is passed to
// NewSessionAuth. SessionAuth owns and validates its own SessionClaims type.
var ErrSessionClaimsUnsupported = errors.New("authn: SessionAuth does not support WithClaims")

// StatusOf maps an authentication error to an HTTP status code and an optional
// WWW-Authenticate challenge. Only errors caused by the client's credentials
// are mapped to 401; infrastructure failures fail closed with 503.
func StatusOf(err error) (code int, challenge string) {
	switch {
	case errors.Is(err, ErrMissingToken):
		return http.StatusUnauthorized, "Bearer"
	case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrSessionInvalid):
		return http.StatusUnauthorized, `Bearer error="invalid_token"`
	default:
		return http.StatusServiceUnavailable, ""
	}
}
