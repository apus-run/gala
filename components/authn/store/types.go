package store

import (
	"context"
	"time"
)

// Storer is the token storage interface | 令牌存储接口.
type Storer interface {
	// Set stores token data with an expiration time | Set 存储令牌数据并指定过期时间.
	Set(ctx context.Context, accessToken string, val any, expiration time.Duration) error

	// Delete removes the token data from storage | Delete 从存储中删除令牌数据.
	Delete(ctx context.Context, accessToken string) (bool, error)

	// Check reports whether the token exists | Check 判断令牌是否存在.
	Check(ctx context.Context, accessToken string) (bool, error)

	// Close closes the storage connection | Close 关闭存储连接.
	Close() error
}
