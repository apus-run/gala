package token

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/pkg/jwtx"
	"github.com/apus-run/gala/pkg/option"
)

func NewJwtManager[T any](opts ...option.Option[JwtConfig]) *JwtManager[T] {
	return &JwtManager[T]{conf: NewJwtConfig(opts...)}
}

func (jm *JwtManager[T]) GenerateToken(userID string, data T) (string, error) {
	registered, err := jm.NewRegisteredClaims(userID, time.Now())
	if err != nil {
		return "", err
	}
	return jm.GenerateClaims(&Claims[T]{Data: data, RegisteredClaims: registered})
}

// NewRegisteredClaims 按管理器配置创建通用声明，供调用方组合自己的顶层载荷。
func (jm *JwtManager[T]) NewRegisteredClaims(userID string, now time.Time) (jwt.RegisteredClaims, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return jwt.RegisteredClaims{}, ErrEmptyUserID
	}

	if jm == nil || jm.conf == nil {
		return jwt.RegisteredClaims{}, ErrEmptyJWTSecret
	}

	if now.IsZero() {
		now = time.Now()
	}
	return jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(now.Add(jm.conf.Expiry)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Issuer:    jm.conf.Issuer,
		Audience:  append(jwt.ClaimStrings(nil), jm.conf.Audience...),
		Subject:   userID,
		ID:        uuid.NewV7().String(),
	}, nil
}

// GenerateClaims 签发调用方提供的顶层声明，不额外包装 data 字段。
func (jm *JwtManager[T]) GenerateClaims(claims jwt.Claims) (string, error) {
	if claims == nil {
		return "", ErrInvalidToken
	}
	secret, signingMethod, err := jm.signingConfig()
	if err != nil {
		return "", err
	}
	return jwtx.GenerateToken(
		func(*jwt.Token) (any, error) {
			return secret, nil
		},
		jwtx.WithClaims(func() jwt.Claims {
			return claims
		}),
		jwtx.WithSigningMethod(signingMethod),
		jwtx.WithTokenHeader(jm.conf.TokenHeader),
	)
}

func (jm *JwtManager[T]) ParseToken(tokenStr string) (*Claims[T], error) {
	claims := &Claims[T]{}
	if err := jm.ParseClaims(tokenStr, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// ParseClaims 校验签名、配置的算法与声明，并将结果写入调用方的 claims 指针。
// 配置里声明过的 Issuer/Audience 由验证方校验 —— 签发时写进 claims 不算数，否则
// 另一个服务用同一个密钥签的 token 也能过。Issuer 有默认值，所以默认就在校验；
// 单次调用传 jwt.WithIssuer("") 可以关掉（jwt/v5 对空的期望值不做校验）。
// opts 追加在配置之后，可以覆盖，用于按调用补充有效期、时钟、leeway 等校验。
func (jm *JwtManager[T]) ParseClaims(tokenStr string, claims jwt.Claims, opts ...jwt.ParserOption) error {
	if strings.TrimSpace(tokenStr) == "" {
		return ErrEmptyToken
	}
	if claims == nil {
		return ErrInvalidToken
	}
	secret, signingMethod, err := jm.signingConfig()
	if err != nil {
		return err
	}

	parserOptions := make([]jwt.ParserOption, 0, len(jm.conf.ParserOptions)+len(opts)+2)
	if jm.conf.Issuer != "" {
		parserOptions = append(parserOptions, jwt.WithIssuer(jm.conf.Issuer))
	}
	if len(jm.conf.Audience) > 0 {
		parserOptions = append(parserOptions, jwt.WithAudience(jm.conf.Audience...))
	}
	parserOptions = append(parserOptions, jm.conf.ParserOptions...)
	parserOptions = append(parserOptions, opts...)

	// 交给 jwtx：算法在 keyFunc 之前就卡住，错误带原因链（errors.Is(err, jwt.ErrTokenExpired) 可用）
	if _, err := jwtx.ParseToken(tokenStr,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwtx.WithClaims(func() jwt.Claims { return claims }),
		jwtx.WithSigningMethod(signingMethod),
		jwtx.WithParserOptions(parserOptions...),
	); err != nil {
		return fmt.Errorf("failed to parse jwt token: %w", err)
	}
	return nil
}

func (jm *JwtManager[T]) VerifyToken(tokenStr string) (bool, error) {
	if _, err := jm.ParseToken(tokenStr); err != nil {
		return false, fmt.Errorf("failed to verify token: %w", err)
	}
	return true, nil
}

func (jm *JwtManager[T]) signingConfig() ([]byte, jwt.SigningMethod, error) {
	if jm == nil || jm.conf == nil {
		return nil, nil, ErrEmptyJWTSecret
	}
	secret := strings.TrimSpace(jm.conf.SecretKey)
	if secret == "" {
		return nil, nil, ErrEmptyJWTSecret
	}
	method, err := jwtSigningMethod(jm.conf.Algorithm)
	if err != nil {
		return nil, nil, err
	}
	return []byte(secret), method, nil
}

func GenerateJWTSecret(length int) string {
	if length <= 0 {
		length = JWTSecretLength
	}
	return hex.EncodeToString(jwtx.GenerateJWTSecret(length))
}

// 只支持 HS*：SecretKey 是共享密钥，配 RS/ES 之类没有对应密钥可用，
// 早报错而不是悄悄换算法（之前会返回 ES256）
func jwtSigningMethod(algorithm string) (jwt.SigningMethod, error) {
	switch strings.ToUpper(strings.TrimSpace(algorithm)) {
	case "HS256", "":
		return jwt.SigningMethodHS256, nil
	case "HS384":
		return jwt.SigningMethodHS384, nil
	case "HS512":
		return jwt.SigningMethodHS512, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlgorithm, algorithm)
	}
}
