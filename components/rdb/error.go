package rdb

import (
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
)

func IsNilError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, redis.Nil)
}
