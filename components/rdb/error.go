package rdb

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrNilOptions is returned when the redis options are nil.
	ErrNilOptions = errors.New("redis options is nil")
	// ErrNilClient is returned when the redis client is nil.
	ErrNilClient = errors.New("redis client is nil")
)

// IsNilError reports whether err is redis.Nil, i.e. the key does not exist.
func IsNilError(err error) bool {
	return errors.Is(err, redis.Nil)
}

// IsClosedError reports whether err means the client has already been closed.
func IsClosedError(err error) bool {
	return errors.Is(err, redis.ErrClosed)
}
