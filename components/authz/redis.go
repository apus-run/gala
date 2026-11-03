package authz

import (
	"errors"
	"fmt"

	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

// NewRedis uses client for Redis policy storage, overriding the configured adapter.
// The default key is casbin_rules. The caller owns client; NewRedis does not close it.
// NewRedis 用 client 做 Redis 策略存储，覆盖已配置的适配器。
// 默认键为 casbin_rules。client 由调用方持有，NewRedis 不会关闭它。
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
