package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store redis storage.
type Store struct {
	cli *redis.Client

	prefix string
}

// NewStore create an *Store instance to handle token storage, deletion, and checking.
func NewStore(client *redis.Client, prefix string) *Store {
	return &Store{cli: client, prefix: prefix}
}

// Set call the Redis client to set a key-value pair with an
// expiration time, where the key name format is <prefix><accessToken>.
func (s *Store) Set(ctx context.Context, accessToken string, val any, expiration time.Duration) error {
	cmd := s.cli.Set(ctx, s.key(accessToken), val, expiration)
	return cmd.Err()
}

// Delete delete the specified JWT Token in Redis.
func (s *Store) Delete(ctx context.Context, accessToken string) (bool, error) {
	cmd := s.cli.Del(ctx, s.key(accessToken))
	if err := cmd.Err(); err != nil {
		return false, err
	}
	return cmd.Val() > 0, nil
}

// Check check if the specified JWT Token exists in Redis.
func (s *Store) Check(ctx context.Context, accessToken string) (bool, error) {
	s.cli.Get(ctx, s.key(accessToken))

	cmd := s.cli.Exists(ctx, s.key(accessToken))
	if err := cmd.Err(); err != nil {
		return false, err
	}
	return cmd.Val() > 0, nil
}

func (s *Store) Close() error {
	return s.cli.Close()
}

// wrapperKey is used to build the key name in Redis.
func (s *Store) key(key string) string {
	return fmt.Sprintf("%s%s", s.prefix, key)
}
