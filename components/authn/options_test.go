package authn

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"github.com/apus-run/gala/components/token"
)

func TestDefaultOptionsIsIndependent(t *testing.T) {
	a, b := DefaultOptions(), DefaultOptions()
	assert.NotSame(t, a, b)
	assert.Equal(t, AccessTokenTTL, a.accessTTL)
	assert.Equal(t, RefreshTokenTTL, a.refreshTTL)
}

func TestApplyKeepsOrder(t *testing.T) {
	o := NewOptions(WithSessionTTL(time.Second, 2*time.Second), WithSessionTTL(3*time.Second, 4*time.Second))
	assert.Equal(t, 3*time.Second, o.accessTTL)
	assert.Equal(t, 4*time.Second, o.refreshTTL)
}

func TestApplyDoesNotResetUntouchedFields(t *testing.T) {
	o := NewOptions(WithSessionTTL(time.Second, 2*time.Second))
	o.Apply(WithClaims(func() jwt.Claims { return jwt.MapClaims{} }))
	o.Apply()
	assert.Equal(t, time.Second, o.accessTTL)
	assert.Equal(t, 2*time.Second, o.refreshTTL)
	assert.NotNil(t, o.claims)
}

func TestNewOptionsDoesNotValidate(t *testing.T) {
	// 非法 TTL 只在 NewSessionAuth 中拒绝，NewOptions 本身不报错。
	o := NewOptions(WithSessionTTL(0, 0))
	assert.Equal(t, time.Duration(0), o.accessTTL)
	assert.Equal(t, time.Duration(0), o.refreshTTL)
}

func TestWithClaimsAndJWTOptionsAccumulate(t *testing.T) {
	factory := func() jwt.Claims { return jwt.MapClaims{} }
	o := NewOptions(WithClaims(factory), WithJWTOptions(token.WithSecretKey(testSecret)), WithJWTOptions(token.WithIssuer("gala")))
	assert.NotNil(t, o.claims)
	assert.Equal(t, jwt.MapClaims{}, o.claims())
	assert.Len(t, o.jwtOpts, 2)
}

func TestOptionsValidateSessionTTL(t *testing.T) {
	tests := []struct {
		name    string
		access  time.Duration
		refresh time.Duration
		ok      bool
	}{
		{"min access equals refresh", time.Second, time.Second, true},
		{"normal pair", 15 * time.Minute, 7 * 24 * time.Hour, true},
		{"access below one second", 500 * time.Millisecond, time.Hour, false},
		{"refresh shorter than access", time.Hour, time.Minute, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewOptions(WithSessionTTL(tt.access, tt.refresh)).Validate()
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
