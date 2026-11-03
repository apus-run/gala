package authn

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/token"
)

const (
	// AccessTokenTTL is the default access token lifetime | 访问令牌的默认存活时间.
	AccessTokenTTL = 15 * time.Minute
	// RefreshTokenTTL is the default refresh token lifetime | 刷新令牌的默认存活时间.
	RefreshTokenTTL = 7 * 24 * time.Hour
)

// ErrSessionInvalid means the session is expired, revoked, or already rotated
// | ErrSessionInvalid 表示会话已过期、已吊销或已轮换.
var ErrSessionInvalid = errors.New("session expired, revoked, or already rotated")

// SessionClaims binds a token to one generation of a login session.
// Subject identifies the user; business permissions remain the caller's concern.
// SessionClaims 把令牌绑定到登录会话的某一代。
// Subject 标识用户；业务权限仍归调用方负责。
type SessionClaims struct {
	jwt.RegisteredClaims
	// SessionID identifies the login session | 登录会话标识.
	SessionID string `json:"sid"`
	// Generation is the rotation generation this token belongs to | 该令牌所属的轮换代次.
	Generation string `json:"generation"`
	// TokenUse is the token purpose: "access" or "refresh" | 令牌用途："access" 或 "refresh".
	TokenUse string `json:"token_use"`
}

// TokenPair is the token pair issued to one login session | 一次登录会话签发出的令牌对.
type TokenPair struct {
	// AccessToken is the short-lived API token | 短寿命的 API 令牌.
	AccessToken string `json:"access_token"`
	// RefreshToken rotates the pair | 用于轮换令牌对的刷新令牌.
	RefreshToken string `json:"refresh_token"`
	// TokenType is the scheme, always "Bearer" | 令牌类型，固定为 "Bearer".
	TokenType string `json:"token_type"`
	// ExpiresIn is the access token lifetime in seconds | 访问令牌寿命（秒）.
	ExpiresIn int64 `json:"expires_in"`
}

// SessionAuth manages token pairs. API middleware must use ParseAccess rather
// than the stateless token parser or the legacy JwtAuth parser for these tokens.
// WithClaims is not used: session claims are owned by this manager.
// SessionAuth 管理令牌对。API 中间件必须用 ParseAccess，而不是无状态解析器或
// 旧版 JwtAuth 的解析器。不使用 WithClaims：会话 claims 归本管理器所有。
type SessionAuth struct {
	manager               *token.JwtManager[struct{}]
	store                 store.SessionStorer
	accessTTL, refreshTTL time.Duration
}

// NewSessionAuth builds a SessionAuth over a mandatory session store. A nil
// store, a WithClaims option, or invalid TTLs make it return an error.
// NewSessionAuth 基于必需的会话存储构建 SessionAuth。
// store 为 nil、传入 WithClaims 或 TTL 校验失败都会返回错误。
func NewSessionAuth(s store.SessionStorer, opts ...Option) (*SessionAuth, error) {
	if s == nil {
		return nil, ErrEmptySessionStore
	}
	o := NewOptions(opts...)
	if o.claims != nil {
		return nil, ErrSessionClaimsUnsupported
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return &SessionAuth{manager: token.NewJwtManager[struct{}](o.jwtOpts...), store: s, accessTTL: o.accessTTL, refreshTTL: o.refreshTTL}, nil
}

// Issue creates an independent login session. Authenticate the user before calling.
// Issue 创建一个独立的登录会话。调用前请先完成用户认证。
func (s *SessionAuth) Issue(ctx context.Context, subject string) (*TokenPair, error) {
	id, generation := rand.Text(), rand.Text()
	pair, expiry, err := s.signPair(subject, id, generation)
	if err != nil {
		return nil, err
	}
	ok, err := s.store.CreateSession(ctx, id, generation, time.Until(expiry))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionInvalid
	}
	return pair, nil
}

// Refresh consumes the current refresh token once and revokes both old tokens.
// A successful rotation starts a new refresh lifetime. Concurrent losers fail;
// replay does not revoke the winning generation. Storage errors fail closed.
// Refresh 一次性消费当前刷新令牌并吊销两个旧令牌。
// 轮换成功即开启新的刷新寿命。并发失败方报错；重放不会吊销获胜的那一代。
// 存储错误一律失败关闭。
func (s *SessionAuth) Refresh(ctx context.Context, raw string) (*TokenPair, error) {
	claims, err := s.parse(raw, "refresh")
	if err != nil {
		return nil, err
	}
	next := rand.Text()
	pair, expiry, err := s.signPair(claims.Subject, claims.SessionID, next)
	if err != nil {
		return nil, err
	}
	ok, err := s.store.RotateSession(ctx, claims.SessionID, claims.Generation, next, time.Until(expiry))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionInvalid
	}
	return pair, nil
}

// ParseAccess verifies an access token against the live session generation
// | ParseAccess 校验访问令牌并对照当前会话代次.
func (s *SessionAuth) ParseAccess(ctx context.Context, raw string) (*SessionClaims, error) {
	claims, err := s.parse(raw, "access")
	if err != nil {
		return nil, err
	}
	ok, err := s.store.MatchSession(ctx, claims.SessionID, claims.Generation)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSessionInvalid
	}
	return claims, nil
}

// Logout revokes the entire session, including tokens from a concurrent refresh.
// Any unexpired, correctly signed token of this session can log it out, even an
// older generation. Repeating logout is harmless. Other login sessions survive.
// Logout 吊销整个会话，包括并发刷新产生的令牌。
// 本会话任何未过期且签名正确的令牌都能登出，旧代次也算。重复登出无害。
// 其他登录会话不受影响。
func (s *SessionAuth) Logout(ctx context.Context, raw string) error {
	claims, err := s.parse(raw, "")
	if err != nil {
		return err
	}
	return s.store.DeleteSession(ctx, claims.SessionID)
}

func (s *SessionAuth) parse(raw, use string) (*SessionClaims, error) {
	// An empty token is a client credential error. Normalize it here so every
	// ParseAccess failure remains recognizable as ErrTokenInvalid by StatusOf.
	// 空令牌是客户端凭据错误。在这里归一化，让 StatusOf 始终能把
	// ParseAccess 的失败识别为 ErrTokenInvalid。
	if strings.TrimSpace(raw) == "" {
		return nil, ErrTokenInvalid
	}
	claims := &SessionClaims{}
	if err := s.manager.ParseClaims(raw, claims, jwt.WithExpirationRequired()); err != nil {
		return nil, err
	}
	if claims.Subject == "" || claims.ID == "" || claims.SessionID == "" || claims.Generation == "" ||
		(claims.TokenUse != "access" && claims.TokenUse != "refresh") || (use != "" && claims.TokenUse != use) {
		return nil, ErrTokenInvalid
	}
	// Session lifetime is strict even when JWT parser leeway is configured.
	// 即使配置了 JWT 解析器的 leeway，会话寿命仍然从严判断。
	if claims.ExpiresAt == nil || !claims.ExpiresAt.After(time.Now()) {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

func (s *SessionAuth) signPair(subject, id, generation string) (*TokenPair, time.Time, error) {
	now := time.Now()
	expiry := now.Add(s.refreshTTL).Truncate(time.Second)
	sign := func(use string, expires time.Time) (string, error) {
		registered, err := s.manager.NewRegisteredClaims(subject, now)
		if err != nil {
			return "", err
		}
		registered.ExpiresAt = jwt.NewNumericDate(expires)
		return s.manager.GenerateClaims(&SessionClaims{RegisteredClaims: registered, SessionID: id, Generation: generation, TokenUse: use})
	}
	access, err := sign("access", now.Add(s.accessTTL))
	if err != nil {
		return nil, time.Time{}, err
	}
	refresh, err := sign("refresh", expiry)
	if err != nil {
		return nil, time.Time{}, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresIn: int64(s.accessTTL.Seconds())}, expiry, nil
}
