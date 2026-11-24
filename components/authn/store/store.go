// Package store defines the token and session storage contracts for authn.
// Implementations live in subpackages — store/redis, store/postgres,
// store/mysql and store/memory — and any other backend only needs to satisfy
// the four SessionStorer methods. Run store/storetest against a custom
// implementation to prove it honours the rules SessionAuth depends on.
// Package store 定义 authn 的令牌与会话存储契约。实现位于子包 store/redis、
// store/postgres、store/mysql 与 store/memory；其他后端只需实现 SessionStorer
// 的四个方法。自定义实现应运行 store/storetest，证明其满足 SessionAuth 依赖的规则。
//
// # Notes | 说明
//
// [Storer] covers access-token KV operations (Set / Delete / Check).
// [SessionStorer] keeps a session's current generation. CreateSession must be
// compare-and-set, and RotateSession must be atomic with respect to concurrent
// rotations and deletion and must never resurrect a missing or expired session.
// [Storer] 覆盖访问令牌的 KV 操作（Set / Delete / Check）。
// [SessionStorer] 保存会话的当前代次。CreateSession 必须是比较并写入；
// RotateSession 相对并发轮换与删除必须原子，且绝不能复活不存在或已过期的会话。
//
// # Example | 示例
//
//	// st is any store.SessionStorer, e.g. redisstore.NewStore(client, "authn:").
//	ok, err := st.RotateSession(ctx, id, old, next, time.Hour)
package store

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidTTL means CreateSession received a non-positive TTL. Every backend
// returns it wrapped or bare, and storetest asserts it, so callers can rely on
// errors.Is instead of matching message text.
// ErrInvalidTTL 表示 CreateSession 收到了非正的 TTL。所有后端都返回它（可能被包装），
// storetest 也这样断言，所以调用方可以依赖 errors.Is，不必匹配错误文本。
var ErrInvalidTTL = errors.New("session TTL must be positive")

// ErrTTLTooSmall means RotateSession received a TTL below one millisecond.
// ErrTTLTooSmall 表示 RotateSession 收到了小于一毫秒的 TTL。
var ErrTTLTooSmall = errors.New("session TTL must be at least one millisecond")

// Storer is the token-revocation storage interface | 令牌吊销存储接口.
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
