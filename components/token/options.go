package token

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/pkg/option"
)

const (
	// DefaultJWTAlgorithm is the default signing algorithm | 默认签名算法.
	DefaultJWTAlgorithm = "HS256"
	// DefaultIssuer is the default issuer | 默认签发者.
	DefaultIssuer = "gala/token"
	// DefaultJWTExpiry is the default token lifetime | 默认令牌有效期.
	DefaultJWTExpiry = 7 * 24 * time.Hour
	// JWTSecretLength is the default secret length in bytes | 默认密钥字节数.
	JWTSecretLength = 32
	// DefaultCookieName is the default token cookie name | 默认 token cookie 名.
	DefaultCookieName = "token"
)

// NewJwtConfig builds a JwtConfig from the defaults plus opts | NewJwtConfig 从默认配置出发应用 opts 构造 JwtConfig.
func NewJwtConfig(opts ...option.Option[JwtConfig]) *JwtConfig {
	config := DefaultJwtConfig()

	option.Apply(config, opts...)

	return config
}

// DefaultJwtConfig ships no secret: a missing secret means ErrEmptyJWTSecret instead of
// signing tokens with a well-known value. CookieSecure defaults to on: local http
// debugging must pass WithCookieSecure(false) explicitly, otherwise browsers drop the cookie.
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

// WithSecretKey sets the shared HMAC secret | WithSecretKey 设置共享 HMAC 密钥.
func WithSecretKey(secretKey string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.SecretKey = secretKey
	}
}

// WithIssuer sets the issuer; empty falls back to the default | WithIssuer 设置签发者，空值回落默认.
func WithIssuer(issuer string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if strings.TrimSpace(issuer) == "" {
			issuer = DefaultIssuer
		}
		c.Issuer = issuer
	}
}

// WithAudience sets the expected audience | WithAudience 设置受众.
func WithAudience(audience ...string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.Audience = append([]string(nil), audience...)
	}
}

// WithAlgorithm sets the signing algorithm (HS* only); empty falls back to the default | WithAlgorithm 设置签名算法（仅 HS*），空值回落默认.
func WithAlgorithm(algorithm string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if strings.TrimSpace(algorithm) == "" {
			algorithm = DefaultJWTAlgorithm
		}
		c.Algorithm = algorithm
	}
}

// WithExpiry sets the token lifetime; zero falls back to the default | WithExpiry 设置令牌有效期，零值回落默认.
func WithExpiry(expiry time.Duration) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if expiry == 0 {
			expiry = DefaultJWTExpiry
		}
		c.Expiry = expiry
	}
}

// WithParserOptions sets the claim-validation rules shared by all parse calls | WithParserOptions 设置所有解析调用共用的声明校验规则.
func WithParserOptions(opts ...jwt.ParserOption) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.ParserOptions = append([]jwt.ParserOption(nil), opts...)
	}
}

// WithJwtConfig overrides with a whole JwtConfig; empty fields fall back to defaults | WithJwtConfig 用整份 JwtConfig 覆盖，空字段回落到默认值.
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

// WithCookieName sets the browser cookie name (default DefaultCookieName); only used by Set/Get/Reset.
// WithCookieName 设置浏览器 cookie 名（默认 DefaultCookieName，见常量），只在 Set/Get/Reset 里用
func WithCookieName(name string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		if strings.TrimSpace(name) == "" {
			name = DefaultCookieName
		}
		c.CookieName = name
	}
}

// WithCookieDomain sets the cookie Domain (default empty = current host only).
// WithCookieDomain 设置 cookie 的 Domain（默认空 = 只对当前 host 生效）
func WithCookieDomain(domain string) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.CookieDomain = domain
	}
}

// WithCookieSecure sets the cookie Secure flag (default true). Only local http debugging should turn it off.
// WithCookieSecure 设置 cookie 的 Secure（默认 true）。只有本地 http 调试才该关掉
func WithCookieSecure(secure bool) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.CookieSecure = secure
	}
}

// WithSameSite sets the cookie SameSite (default Lax), using the standard http.SameSite rather than a new enum.
// WithSameSite 设置 cookie 的 SameSite（默认 Lax）。用标准库的 http.SameSite，不另造枚举
func WithSameSite(sameSite http.SameSite) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.SameSite = sameSite
	}
}

// WithDisableXSRF turns off the double-submit XSRF check for cookie requests. Use only when CSRF is handled elsewhere.
// WithDisableXSRF 关掉 cookie 请求的 XSRF 双提交校验。仅在别处已有 CSRF 防护时才用
func WithDisableXSRF(disable bool) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.DisableXSRF = disable
	}
}

// WithTokenHeader adds custom fields to the JWT header (usually kid, to pick a key during rotation).
// WithTokenHeader 往 JWT header 里塞自定义字段（通常是 kid，供密钥轮换时选密钥）
func WithTokenHeader(header map[string]any) option.Option[JwtConfig] {
	return func(c *JwtConfig) {
		c.TokenHeader = header
	}
}

// jwtConfig converts the TokenConfig JWT* fields into JwtConfig options.
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

// NewTokenConfig builds a TokenConfig from the defaults plus opts | NewTokenConfig 从默认配置出发应用 opts 构造 TokenConfig.
func NewTokenConfig(opts ...option.Option[TokenConfig]) *TokenConfig {
	config := DefaultTokenConfig()

	option.Apply(&config, opts...)

	return &config
}

// DefaultTokenConfig returns the default generator configuration | DefaultTokenConfig 返回默认的生成器配置.
func DefaultTokenConfig() TokenConfig {
	return TokenConfig{
		JWTIssuer:    DefaultIssuer,
		JWTAlgorithm: DefaultJWTAlgorithm,
		JWTExpiry:    DefaultJWTExpiry,
		TokenStyle:   TokenStyleJWT,

		SimpleTokenLength: DefaultSimpleLength,
	}
}

// WithTokenConfig overrides with a whole TokenConfig; empty fields fall back to defaults | WithTokenConfig 用整份 TokenConfig 覆盖，空字段回落到默认值.
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

// WithJWTIssuer sets the JWT issuer; empty falls back to the default | WithJWTIssuer 设置 JWT 签发者，空值回落默认.
func WithJWTIssuer(issuer string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if issuer == "" {
			issuer = DefaultIssuer
		}
		c.JWTIssuer = issuer
	}
}

// WithJWTSecretKey sets the shared HMAC secret | WithJWTSecretKey 设置共享 HMAC 密钥.
func WithJWTSecretKey(secretKey string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		c.JWTSecretKey = secretKey
	}
}

// WithJWTAudience sets the expected audience | WithJWTAudience 设置受众.
func WithJWTAudience(audience ...string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		c.JWTAudience = append([]string(nil), audience...)
	}
}

// WithJWTAlgorithm sets the signing algorithm (HS* only); empty falls back to the default | WithJWTAlgorithm 设置签名算法（仅 HS*），空值回落默认.
func WithJWTAlgorithm(algorithm string) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if algorithm == "" {
			algorithm = DefaultJWTAlgorithm
		}
		c.JWTAlgorithm = algorithm
	}
}

// WithJWTExpiry sets the token lifetime; zero falls back to the default | WithJWTExpiry 设置令牌有效期，零值回落默认.
func WithJWTExpiry(expiry time.Duration) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if expiry == 0 {
			expiry = DefaultJWTExpiry
		}
		c.JWTExpiry = expiry
	}
}

// WithTokenStyle selects the generator style; empty falls back to JWT | WithTokenStyle 选择生成风格，空值回落 JWT.
func WithTokenStyle(style TokenStyle) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if style == "" {
			style = TokenStyleJWT
		}
		c.TokenStyle = style
	}
}

// WithSimpleTokenLength sets the length of simple tokens; non-positive falls back to the default | WithSimpleTokenLength 设置 simple 风格长度，非正值回落默认.
func WithSimpleTokenLength(length int) option.Option[TokenConfig] {
	return func(c *TokenConfig) {
		if length <= 0 {
			length = DefaultSimpleLength
		}
		c.SimpleTokenLength = length
	}
}
