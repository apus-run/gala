package authn

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/pkg/jwtx"

	rdb "github.com/apus-run/gala/components/authn/store"
)

var _ Authenticator = (*JwtAuth)(nil)

// 直接复用 jwtx 的错误，避免两处各定义一份再慢慢漂移
var (
	ErrTokenInvalid           = jwtx.ErrTokenInvalid
	ErrUnSupportSigningMethod = jwtx.ErrUnSupportSigningMethod
	ErrSignToken              = jwtx.ErrSignToken
	ErrGetKey                 = jwtx.ErrGetKey
)

// JwtAuth implement the authn.Authenticator interface.

type JwtAuth struct {
	*Options
	store rdb.Storer
}

func NewJwtAuth(store rdb.Storer, opts ...Option) *JwtAuth {
	options := Apply(opts...)
	return &JwtAuth{
		Options: options,
		store:   store,
	}
}

func (j *JwtAuth) Sign(ctx context.Context) (Token, error) {
	tokenString, err := j.GenerateToken(ctx)
	if err != nil {
		return nil, err
	}
	return &tokenInfo{
		Token:     tokenString,
		Type:      j.tokenType,
		ExpiresAt: time.Now().Add(j.expired).Unix(),
	}, nil
}

func (j *JwtAuth) Destroy(ctx context.Context, refreshToken string) error {
	claims, err := j.ParseClaims(ctx, refreshToken)
	if err != nil {
		return err
	}
	if claims.ExpiresAt == nil {
		return ErrTokenInvalid
	}

	// If storage is set, put the unexpired token in
	store := func(store rdb.Storer) error {
		expired := time.Until(claims.ExpiresAt.Time)
		return store.Set(ctx, refreshToken, "1", expired)
	}
	return j.callStore(store)
}

func (j *JwtAuth) ParseClaims(ctx context.Context, accessToken string) (*jwt.RegisteredClaims, error) {
	if accessToken == "" {
		return nil, ErrTokenInvalid
	}

	token, err := j.ParseToken(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	store := func(store rdb.Storer) error {
		exists, err := store.Check(ctx, accessToken)
		if err != nil {
			return err
		}

		if exists {
			return ErrTokenInvalid
		}

		return nil
	}

	if err := j.callStore(store); err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

func (j *JwtAuth) ParseToken(_ context.Context, accessToken string) (*jwt.Token, error) {
	if accessToken == "" {
		return nil, ErrTokenInvalid
	}
	return jwtx.ParseToken(accessToken, j.keyFunc,
		jwtx.WithSigningMethod(j.signingMethod), jwtx.WithClaims(j.claims))
}

func (j *JwtAuth) GenerateToken(_ context.Context) (string, error) {
	return jwtx.GenerateToken(j.keyFunc,
		jwtx.WithSigningMethod(j.signingMethod), jwtx.WithClaims(j.claims), jwtx.WithTokenHeader(j.tokenHeader))
}

func (j *JwtAuth) callStore(fn func(rdb.Storer) error) error {
	if store := j.store; store != nil {
		return fn(store)
	}
	return nil
}
