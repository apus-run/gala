package rdb

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrNilOptions is returned when the redis options are nil | redis 配置为 nil 时返回 ErrNilOptions.
	ErrNilOptions = errors.New("redis options is nil")
	// ErrNilClient is returned when the redis client is nil | redis 客户端为 nil 时返回 ErrNilClient.
	ErrNilClient = errors.New("redis client is nil")
)

// IsNilError reports whether err is redis.Nil, i.e. the key does not exist | IsNilError 报告 err 是否为 redis.Nil，即 key 不存在.
func IsNilError(err error) bool {
	return errors.Is(err, redis.Nil)
}

// IsClosedError reports whether err means the client has already been closed | IsClosedError 报告 err 是否表示客户端已关闭.
func IsClosedError(err error) bool {
	return errors.Is(err, redis.ErrClosed)
}
