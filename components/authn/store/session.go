package store

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionStorer stores the current generation of a session. Missing sessions
// must never be recreated by RotateSession. Implementations must make rotation
// atomic with respect to other rotations and deletion.
// SessionStorer 存储会话的当前代次。已不存在的会话绝不能被 RotateSession
// 重新创建。实现必须让轮换相对其他轮换与删除是原子的。
type SessionStorer interface {
	// CreateSession creates a session when absent; false means it already exists
	// | CreateSession 在会话不存在时创建；返回 false 表示已存在.
	CreateSession(context.Context, string, string, time.Duration) (bool, error)
	// MatchSession reports whether the generation still matches the stored one
	// | MatchSession 判断代次是否与存储的一致.
	MatchSession(context.Context, string, string) (bool, error)
	// RotateSession swaps the generation only when old matches; false means lost the race
	// | RotateSession 仅在 old 匹配时换代；返回 false 表示没抢到.
	RotateSession(context.Context, string, string, string, time.Duration) (bool, error)
	// DeleteSession removes the whole session | DeleteSession 删除整个会话.
	DeleteSession(context.Context, string) error
}

var _ SessionStorer = (*Store)(nil)

func (s *Store) sessionKey(id string) string { return s.key("session:" + id) }

// CreateSession creates the session generation when absent | CreateSession 在会话不存在时写入代次.
func (s *Store) CreateSession(ctx context.Context, id, generation string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("session TTL must be positive")
	}
	return s.cli.SetNX(ctx, s.sessionKey(id), generation, ttl).Result()
}

// MatchSession reports whether the stored generation matches | MatchSession 判断存储的代次是否匹配.
func (s *Store) MatchSession(ctx context.Context, id, generation string) (bool, error) {
	value, err := s.cli.Get(ctx, s.sessionKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return value == generation && err == nil, err
}

var rotateSession = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
return 1
`)

// RotateSession atomically swaps the generation when old matches | RotateSession 在 old 匹配时原子换代.
func (s *Store) RotateSession(ctx context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	if ttl < time.Millisecond {
		return false, errors.New("session TTL must be at least one millisecond")
	}
	n, err := rotateSession.Run(ctx, s.cli, []string{s.sessionKey(id)}, old, next, ttl.Milliseconds()).Int()
	return n == 1 && err == nil, err
}

// DeleteSession removes the whole session | DeleteSession 删除整个会话.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.cli.Del(ctx, s.sessionKey(id)).Err()
}
