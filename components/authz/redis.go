package authz

import (
	"errors"
	"fmt"

	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

// NewRedis uses client for Redis policy storage, overriding the configured adapter.
// The default key is casbin_rules. The caller owns client; NewRedis does not close it.
func NewRedis(client redis.Cmdable, opts ...Option) (*Authorizer, error) {
	if isNil(client) {
		return nil, errors.New("authz: nil redis client")
	}
	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{Client: client})
	if err != nil {
		return nil, fmt.Errorf("authz: create redis adapter: %w", err)
	}
	opts = append(append([]Option{}, opts...), WithPolicyAdapter(adapter))
	return buildAuthorizer(nil, opts...)
}
