// Package redis provides the Redis implementations of store.Storer and
// store.SessionStorer. It accepts any redis.Cmdable, so single-node, cluster
// and ring clients all work.
// Package redis 提供 store.Storer 与 store.SessionStorer 的 Redis 实现。
// 它接受任意 redis.Cmdable，单机、集群与 ring 客户端都可用。
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn/store"
)

// Store is the Redis-backed token and session storage | 基于 Redis 的令牌与会话存储.
type Store struct {
	cli    redis.Cmdable
	prefix string
}

// NewStore creates a Store over any Redis client. The client does not have to
// implement Close; Close only acts when it does.
// NewStore 基于任意 Redis 客户端创建 Store。客户端不必支持关闭，Close 仅在其
// 支持时生效。
func NewStore(client redis.Cmdable, prefix string) *Store {
	return &Store{cli: client, prefix: prefix}
}

var (
	_ store.Storer        = (*Store)(nil)
	_ store.SessionStorer = (*Store)(nil)
)

// Set calls the Redis client to set a key-value pair with an expiration time;
// the key name format is <prefix><accessToken>.
// Set 调用 Redis 客户端写入带过期时间的键值对，
// 键名格式为 <prefix><accessToken>。
// Set records the token in the revocation list. The opaque value is not stored:
// no getter exists, so only existence and expiry are observable — the same
// rule the SQL backends follow.
// Set 把令牌记入吊销名单。接口没有读回入口，只有存在性与过期可观察，因此不保存
// 那个不透明的值——与 SQL 后端遵循同一规则。
func (s *Store) Set(ctx context.Context, accessToken string, _ any, expiration time.Duration) error {
	return s.cli.Set(ctx, s.key(accessToken), "1", expiration).Err()
}

// Delete removes the specified token from Redis | Delete 删除 Redis 中指定的令牌.
func (s *Store) Delete(ctx context.Context, accessToken string) (bool, error) {
	cmd := s.cli.Del(ctx, s.key(accessToken))
	if err := cmd.Err(); err != nil {
		return false, err
	}
	return cmd.Val() > 0, nil
}

// Check reports whether the specified token exists in Redis
// | Check 判断指定的令牌在 Redis 中是否存在.
func (s *Store) Check(ctx context.Context, accessToken string) (bool, error) {
	cmd := s.cli.Exists(ctx, s.key(accessToken))
	if err := cmd.Err(); err != nil {
		return false, err
	}
	return cmd.Val() > 0, nil
}

// Close closes the underlying client when it exposes Close. Clients owned
// elsewhere are left alone; a mock or proxy may have none.
// Close 在底层客户端提供 Close 时关闭它；客户端归调用方所有时不做处理。
func (s *Store) Close() error {
	if closer, ok := s.cli.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// key builds the key name in Redis | key 用于构造 Redis 中的键名.
func (s *Store) key(key string) string {
	return fmt.Sprintf("%s%s", s.prefix, key)
}
