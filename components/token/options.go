package token

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/pkg/option"
)

const (
	DefaultJWTAlgorithm = "HS256"
	DefaultIssuer       = "gala/token"
	DefaultJWTExpiry    = 7 * 24 * time.Hour
	JWTSecretLength     = 32
	DefaultCookieName   = "token"
)

func NewJwtConfig(opts ...option.Option[JwtConfig]) *JwtConfig {
	config := DefaultJwtConfig()

	option.Apply(config, opts...)

	return config
}

// DefaultJwtConfig 不预置密钥：没配就是 ErrEmptyJWTSecret，而不是拿一个人人皆知的值签 token。
// CookieSecure 默认开：本地 http 调试要显式 WithCookieSecure(false)，否则浏览器不存 cookie。
func DefaultJwtConfig() *JwtConfig {
	return &JwtConfig{
		Issuer:       DefaultIssuer,
		Algorithm:    DefaultJWTAlgorithm,
		Expiry:       DefaultJWTExpiry,
		CookieName:   DefaultCookieName,
		CookieSecure: true,
		SameSite:     http.SameSiteLaxMode,
	}
}

func WithSecretKey(secretKey string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.SecretKey = secretKey
	}
}

func WithIssuer(issuer string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if strings.TrimSpace(issuer) == "" {
			issuer = DefaultIssuer
		}
		c.Issuer = issuer
	}
}

func WithAudience(audience ...string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.Audience = append([]string(nil), audience...)
	}
}

func WithAlgorithm(algorithm string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if strings.TrimSpace(algorithm) == "" {
			algorithm = DefaultJWTAlgorithm
		}
		c.Algorithm = algorithm
	}
}

func WithExpiry(expiry time.Duration) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if expiry == 0 {
			expiry = DefaultJWTExpiry
		}
		c.Expiry = expiry
	}
}

// WithParserOptions 设置所有解析调用共用的声明校验规则。
func WithParserOptions(opts ...jwt.ParserOption) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.ParserOptions = append([]jwt.ParserOption(nil), opts...)
	}
}

// WithJwtConfig 用整份 JwtConfig 覆盖，空字段回落到默认值。
func WithJwtConfig(config JwtConfig) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		WithSecretKey(config.SecretKey)(c)
		WithIssuer(config.Issuer)(c)
		WithAudience(config.Audience...)(c)
		WithAlgorithm(config.Algorithm)(c)
		WithExpiry(config.Expiry)(c)
		WithParserOptions(config.ParserOptions...)(c)
		WithCookieName(config.CookieName)(c)
		WithCookieDomain(config.CookieDomain)(c)
		WithCookieSecure(config.CookieSecure)(c)
		WithSameSite(config.SameSite)(c)
		WithDisableXSRF(config.DisableXSRF)(c)
		WithTokenHeader(config.TokenHeader)(c)
	}
}

// WithCookieName 设置浏览器 cookie 名（默认 DefaultCookieName，见常量），只在 Set/Get/Reset 里用
func WithCookieName(name string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if strings.TrimSpace(name) == "" {
			name = DefaultCookieName
		}
		c.CookieName = name
	}
}

// WithCookieDomain 设置 cookie 的 Domain（默认空 = 只对当前 host 生效）
func WithCookieDomain(domain string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.CookieDomain = domain
	}
}

// WithCookieSecure 设置 cookie 的 Secure（默认 true）。只有本地 http 调试才该关掉
func WithCookieSecure(secure bool) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.CookieSecure = secure
	}
}

// WithSameSite 设置 cookie 的 SameSite（默认 Lax）。用标准库的 http.SameSite，不另造枚举
func WithSameSite(sameSite http.SameSite) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.SameSite = sameSite
	}
}

// WithDisableXSRF 关掉 cookie 请求的 XSRF 双提交校验。仅在别处已有 CSRF 防护时才用
func WithDisableXSRF(disable bool) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.DisableXSRF = disable
	}
}

// WithTokenHeader 往 JWT header 里塞自定义字段（通常是 kid，供密钥轮换时选密钥）
func WithTokenHeader(header map[string]any) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.TokenHeader = header
	}
}

// jwtConfig 把 TokenConfig 里的 JWT* 字段转成 JwtConfig 的选项。
func (c TokenConfig) jwtConfig() option.Option[JwtConfig] {
	return func(jwt *JwtConfig) {
		WithSecretKey(c.JWTSecretKey)(jwt)
		WithIssuer(c.JWTIssuer)(jwt)
		WithAudience(c.JWTAudience...)(jwt)
		WithAlgorithm(c.JWTAlgorithm)(jwt)
		WithExpiry(c.JWTExpiry)(jwt)
	}
}

func NewTokenConfig(opts ...option.Option[TokenConfig]) *TokenConfig {
	config := DefaultTokenConfig()

	option.Apply(&config, opts...)

	return &config
}

func DefaultTokenConfig() TokenConfig {
	return TokenConfig{
		JWTIssuer:    DefaultIssuer,
		JWTAlgorithm: DefaultJWTAlgorithm,
		JWTExpiry:    DefaultJWTExpiry,
		TokenStyle:   TokenStyleJWT,

		SimpleTokenLength: DefaultSimpleLength,
	}
}

// WithTokenConfig 用整份 TokenConfig 覆盖，空字段回落到默认值。
func WithTokenConfig(config TokenConfig) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		WithJWTIssuer(config.JWTIssuer)(c)
		WithJWTSecretKey(config.JWTSecretKey)(c)
		WithJWTAudience(config.JWTAudience...)(c)
		WithJWTAlgorithm(config.JWTAlgorithm)(c)
		WithJWTExpiry(config.JWTExpiry)(c)
		WithTokenStyle(config.TokenStyle)(c)
		WithSimpleTokenLength(config.SimpleTokenLength)(c)
	}
}

func WithJWTIssuer(issuer string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if issuer == "" {
			issuer = DefaultIssuer
		}
		c.JWTIssuer = issuer
	}
}

func WithJWTSecretKey(secretKey string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		c.JWTSecretKey = secretKey
	}
}

func WithJWTAudience(audience ...string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		c.JWTAudience = append([]string(nil), audience...)
	}
}

func WithJWTAlgorithm(algorithm string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if algorithm == "" {
			algorithm = DefaultJWTAlgorithm
		}
		c.JWTAlgorithm = algorithm
	}
}

func WithJWTExpiry(expiry time.Duration) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if expiry == 0 {
			expiry = DefaultJWTExpiry
		}
		c.JWTExpiry = expiry
	}
}

func WithTokenStyle(style TokenStyle) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if style == "" {
			style = TokenStyleJWT
		}
		c.TokenStyle = style
	}
}

func WithSimpleTokenLength(length int) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if length <= 0 {
			length = DefaultSimpleLength
		}
		c.SimpleTokenLength = length
	}
}
