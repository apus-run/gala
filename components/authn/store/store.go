// Package store defines the token-storage contract for authn and ships a
// Redis implementation | 定义 authn 的令牌存储契约，并附带 Redis 实现.
//
// # Notes | 说明
//
// [Storer] covers access-token KV operations (Set / Delete / Check);
// [SessionStorer] keeps a session's current generation. RotateSession must
// never resurrect a deleted session and must be atomic against concurrent
// rotations — the Redis Store implements it with a compare-and-set Lua
// script, and CreateSession with SetNX.
// [Storer] 覆盖访问令牌的 KV 操作（Set / Delete / Check）；[SessionStorer]
// 保存会话的当前代次。RotateSession 绝不能复活已删除的会话，且相对并发轮换
// 必须原子——Redis 的 Store 用比较并换值的 Lua 脚本实现，CreateSession 用 SetNX。
//
// # Example | 示例
//
//	st := store.NewStore(rdb, "authn:")
//	_ = st.Set(ctx, token, claims, time.Hour)
//	ok, err := st.RotateSession(ctx, id, old, next, time.Hour)
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store is the Redis-backed token storage | 基于 Redis 的令牌存储.
type Store struct {
	cli *redis.Client

	prefix string
}

// NewStore creates a *Store to handle token storage, deletion and checking
// | NewStore 创建 *Store 实例，负责令牌的存储、删除与检查.
func NewStore(client *redis.Client, prefix string) *Store {
	return &Store{cli: client, prefix: prefix}
}

// Set calls the Redis client to set a key-value pair with an expiration time;
// the key name format is <prefix><accessToken>.
// Set 调用 Redis 客户端写入带过期时间的键值对，
// 键名格式为 <prefix><accessToken>。
func (s *Store) Set(ctx context.Context, accessToken string, val any, expiration time.Duration) error {
	cmd := s.cli.Set(ctx, s.key(accessToken), val, expiration)
	return cmd.Err()
}

// Delete removes the specified JWT token from Redis | Delete 删除 Redis 中指定的 JWT 令牌.
func (s *Store) Delete(ctx context.Context, accessToken string) (bool, error) {
	cmd := s.cli.Del(ctx, s.key(accessToken))
	if err := cmd.Err(); err != nil {
		return false, err
	}
	return cmd.Val() > 0, nil
}

// Check reports whether the specified JWT token exists in Redis
// | Check 判断指定的 JWT 令牌在 Redis 中是否存在.
func (s *Store) Check(ctx context.Context, accessToken string) (bool, error) {
	cmd := s.cli.Exists(ctx, s.key(accessToken))
	if err := cmd.Err(); err != nil {
		return false, err
	}
	return cmd.Val() > 0, nil
}

// Close closes the underlying Redis connection | Close 关闭底层的 Redis 连接.
func (s *Store) Close() error {
	return s.cli.Close()
}

// key builds the key name in Redis | key 用于构造 Redis 中的键名.
func (s *Store) key(key string) string {
	return fmt.Sprintf("%s%s", s.prefix, key)
}
