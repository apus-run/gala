package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn/store"
)

// sessionKey namespaces sessions apart from revoked tokens under the same prefix.
// sessionKey 把会话与同一前缀下的吊销令牌分开命名。
func (s *Store) sessionKey(id string) string { return s.key("session:" + id) }

// CreateSession creates the session generation when absent | CreateSession 在会话不存在时写入代次.
func (s *Store) CreateSession(ctx context.Context, id, generation string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, store.ErrInvalidTTL
	}
	return s.cli.SetNX(ctx, s.sessionKey(id), generation, ttl).Result()
}

// MatchSession reports whether the stored generation matches | MatchSession 判断存储的代次是否匹配.
func (s *Store) MatchSession(ctx context.Context, id, generation string) (bool, error) {
	value, err := s.cli.Get(ctx, s.sessionKey(id)).Result()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	return value == generation && err == nil, err
}

var rotateSession = goredis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
return 1
`)

// RotateSession atomically swaps the generation when old matches | RotateSession 在 old 匹配时原子换代.
func (s *Store) RotateSession(ctx context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	if ttl < time.Millisecond {
		return false, store.ErrTTLTooSmall
	}
	n, err := rotateSession.Run(ctx, s.cli, []string{s.sessionKey(id)}, old, next, ttl.Milliseconds()).Int()
	return n == 1 && err == nil, err
}

// DeleteSession removes the whole session | DeleteSession 删除整个会话.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.cli.Del(ctx, s.sessionKey(id)).Err()
}
