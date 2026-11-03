package token

import "errors"

var (
	// ErrEmptyUserID means the user id is empty | 用户 ID 为空.
	ErrEmptyUserID = errors.New("user id is empty")
	// ErrEmptyToken means the token string is empty | 令牌串为空.
	ErrEmptyToken = errors.New("token is empty")
	// ErrEmptyJWTSecret means the jwt secret key is empty | JWT 密钥为空.
	ErrEmptyJWTSecret = errors.New("jwt secret key is empty")
	// ErrInvalidToken means the token is invalid | 令牌无效.
	ErrInvalidToken = errors.New("invalid token")
	// ErrUnsupportedTokenStyle means the token style is not supported | 不支持的令牌风格.
	ErrUnsupportedTokenStyle = errors.New("unsupported token style")
	// ErrUnsupportedAlgorithm means the jwt signing algorithm is not supported | 不支持的 JWT 签名算法.
	ErrUnsupportedAlgorithm = errors.New("unsupported jwt signing algorithm")
	// ErrGenerateToken means token generation failed | 令牌生成失败.
	ErrGenerateToken = errors.New("failed to generate token")
	// ErrMissingToken means no token in the Authorization header or cookie | Authorization 头与 cookie 中都没有令牌.
	ErrMissingToken = errors.New("no token in Authorization header or cookie")
	// ErrXSRFMismatch means the xsrf token does not match | XSRF 令牌不匹配.
	ErrXSRFMismatch = errors.New("xsrf token mismatch")
)
