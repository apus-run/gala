package memory

import (
	"context"
	"sync"
	"time"

	"github.com/apus-run/gala/components/authn/store"
)

// Store is an in-memory store.SessionStorer for tests and single-process use.
// It follows the same contract as the Redis Store: creation is compare-and-set,
// rotation never resurrects a missing or expired session, and both are atomic.
// Data does not survive a restart and is not shared between processes — use
// the Redis Store in production.
// Store 是供测试与单进程使用的内存 store.SessionStorer。
// 它与 Redis Store 遵循同一契约：创建是比较并写入，轮换不会复活不存在或已
// 过期的会话，两者都是原子的。数据不跨重启、不跨进程——生产请用 Redis Store。
type Store struct {
	mu      sync.Mutex
	entries map[string]memorySession
}

type memorySession struct {
	generation string
	expiresAt  time.Time
}

// NewStore creates an empty in-memory store | NewStore 创建空的内存存储.
func NewStore() *Store {
	return &Store{entries: make(map[string]memorySession)}
}

// CreateSession creates the session generation when absent | CreateSession 在会话不存在时写入代次.
func (m *Store) CreateSession(_ context.Context, id, generation string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, store.ErrInvalidTTL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if entry, ok := m.entries[id]; ok && now.Before(entry.expiresAt) {
		return false, nil
	}
	m.entries[id] = memorySession{generation: generation, expiresAt: now.Add(ttl)}
	return true, nil
}

// MatchSession reports whether the generation still matches the stored one
// | MatchSession 判断代次是否与存储的一致.
func (m *Store) MatchSession(_ context.Context, id, generation string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[id]
	if !ok || !time.Now().Before(entry.expiresAt) {
		delete(m.entries, id)
		return false, nil
	}
	return entry.generation == generation, nil
}

// RotateSession swaps the generation only when old matches; false means lost the race
// | RotateSession 仅在 old 匹配时换代；返回 false 表示没抢到.
func (m *Store) RotateSession(_ context.Context, id, old, next string, ttl time.Duration) (bool, error) {
	if ttl < time.Millisecond {
		return false, store.ErrTTLTooSmall
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	entry, ok := m.entries[id]
	if !ok || !now.Before(entry.expiresAt) {
		delete(m.entries, id)
		return false, nil
	}
	if entry.generation != old {
		return false, nil
	}
	m.entries[id] = memorySession{generation: next, expiresAt: now.Add(ttl)}
	return true, nil
}

// DeleteSession removes the whole session | DeleteSession 删除整个会话.
func (m *Store) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, id)
	return nil
}

var _ store.SessionStorer = (*Store)(nil)
