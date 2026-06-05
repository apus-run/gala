package rdb

import (
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
)

var ErrNilOptions = errors.New("redis options is nil")

func IsNilError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, redis.Nil)
}

func IsClosedError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, redis.ErrClosed)
}
