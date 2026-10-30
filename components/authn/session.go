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
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
)

var ErrSessionInvalid = errors.New("session expired, revoked, or already rotated")

// SessionClaims binds a token to one generation of a login session.
// Subject identifies the user; business permissions remain the caller's concern.
type SessionClaims struct {
	jwt.RegisteredClaims
	SessionID  string `json:"sid"`
	Generation string `json:"generation"`
	TokenUse   string `json:"token_use"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// SessionAuth manages token pairs. API middleware must use ParseAccess rather
// than the stateless token parser or the legacy JwtAuth parser for these tokens.
// WithClaims is not used: session claims are owned by this manager.
type SessionAuth struct {
	manager               *token.JwtManager[struct{}]
	store                 store.SessionStorer
	accessTTL, refreshTTL time.Duration
}

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
