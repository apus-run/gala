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
type SessionStorer interface {
	CreateSession(context.Context, string, string, time.Duration) (bool, error)
	MatchSession(context.Context, string, string) (bool, error)
	RotateSession(context.Context, string, string, string, time.Duration) (bool, error)
	DeleteSession(context.Context, string) error
}

var _ SessionStorer = (*Store)(nil)

func (s *Store) sessionKey(id string) string { return s.key("session:" + id) }

func (s *Store) CreateSession(ctx context.Context, id, generation string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, errors.New("session TTL must be positive")
	}
	return s.cli.SetNX(ctx, s.sessionKey(id), generation, ttl).Result()
}

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

func (s *Store) RotateSession(ctx context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	if ttl < time.Millisecond {
		return false, errors.New("session TTL must be at least one millisecond")
	}
	n, err := rotateSession.Run(ctx, s.cli, []string{s.sessionKey(id)}, old, next, ttl.Milliseconds()).Int()
	return n == 1 && err == nil, err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.cli.Del(ctx, s.sessionKey(id)).Err()
}
