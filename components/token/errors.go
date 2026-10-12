package token

import "errors"

var (
	ErrEmptyUserID           = errors.New("user id is empty")
	ErrEmptyToken            = errors.New("token is empty")
	ErrEmptyJWTSecret        = errors.New("jwt secret key is empty")
	ErrInvalidToken          = errors.New("invalid token")
	ErrUnsupportedTokenStyle = errors.New("unsupported token style")
	ErrUnsupportedAlgorithm  = errors.New("unsupported jwt signing algorithm")
	ErrGenerateToken         = errors.New("failed to generate token")
	ErrMissingToken          = errors.New("no token in Authorization header or cookie")
	ErrXSRFMismatch          = errors.New("xsrf token mismatch")
)
