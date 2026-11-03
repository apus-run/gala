// Package ratelimit provides a Redis-backed sliding-window rate limiter with a
// local in-memory fallback when Redis is unavailable | 基于 Redis 的滑动窗口限流器，Redis 不可用时回退本地内存.
//
// # Notes | 说明
//
// The Redis path runs one Lua script over a ZSET: prune expired members, count
// the window, record the hit — atomically. When Redis returns an error, Allow
// silently falls back to a per-process in-memory window, so limits hold
// per-instance, not globally. Local entries are only evicted by StartCleanup;
// without it every key ever seen keeps an entry forever. max is read on each
// Allow call (max <= 0 disables limiting), so budgets can vary per call.
// Redis 路径用一个 Lua 脚本操作 ZSET：清理过期成员、统计窗口、记录请求，全程原子。
// Redis 出错时 Allow 静默回退到进程内内存窗口，限流只对本进程有效而非全局。本地条目
// 仅靠 StartCleanup 清理，不调用则每个出现过的 key 永久占用一个条目。max 在每次
// Allow 时读取（max <= 0 表示不限流），预算可按次变化。
//
// # Features | 功能
//
// - Sliding-window Allow per key, budget passed per call | 按 key 滑动窗口限流，预算按次传入
// - Atomic ZSET Lua script on the Redis side | Redis 侧原子的 ZSET Lua 脚本
// - In-memory fallback when Redis fails | Redis 故障时的进程内回退
// - StartCleanup evicts expired local entries | StartCleanup 定期清理过期本地条目
//
// # Example | 示例
//
//	l := ratelimit.New(redisClient, "api:rl:", time.Minute, "")
//	go l.StartCleanup(stop)
//	if !l.Allow(ctx, clientIP, 100) {
//		http.Error(w, "too many requests", http.StatusTooManyRequests)
//	}
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/redis/go-redis/v9"
)

const localCleanupInterval = time.Minute

// rateLimitScript atomically prunes expired ZSET members, checks the count,
// and conditionally records a new hit.
// rateLimitScript 原子地清理过期 ZSET 成员、检查计数，并按条件记录一次新请求。
var rateLimitScript = redis.NewScript(`
local key     = KEYS[1]
local now     = tonumber(ARGV[1])
local window  = tonumber(ARGV[2])
local maxReq  = tonumber(ARGV[3])
local member  = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count < maxReq then
    redis.call('ZADD', key, now, member)
    redis.call('PEXPIRE', key, window + 1000)
    return 1
end
return 0
`)

// Limiter enforces per-key sliding-window limits. max is evaluated per Allow
// call so callers (e.g. embed channels) can vary budgets without rebuilding
// the limiter.
// Limiter 按 key 施加滑动窗口限流。max 在每次 Allow 调用时求值，调用方（如 embed
// 频道）无需重建 limiter 即可改变预算。
type Limiter struct {
	redis     *redis.Client
	local     *localLimiter
	keyPrefix string
	window    time.Duration
	memberID  string
	now       func() time.Time
	sequence  atomic.Uint64
}

// New constructs a limiter. keyPrefix should include a trailing delimiter
// (e.g. "embed:ratelimit:"). When redis is nil, only the local fallback runs.
// New 构造一个 limiter。keyPrefix 应包含结尾分隔符（如 "embed:ratelimit:"）。
// redis 为 nil 时只运行本地回退。
func New(redisClient *redis.Client, keyPrefix string, window time.Duration, instanceID string) *Limiter {
	if window <= 0 {
		window = time.Minute
	} else if window < time.Millisecond {
		// Redis scores and expiration use milliseconds. Keep both backends on the
		// same minimum precision instead of silently turning this into a 0ms window.
		// Redis 的分值与过期时间以毫秒为单位。让两个后端保持相同的最小精度，
		// 而不是悄悄变成 0ms 窗口。
		window = time.Millisecond
	}
	if instanceID == "" {
		instanceID = uuid.New().String()
	}
	return &Limiter{
		redis:     redisClient,
		local:     newLocalLimiter(),
		keyPrefix: keyPrefix,
		window:    window,
		memberID:  instanceID + ":" + uuid.New().String(),
		now:       time.Now,
	}
}

// Allow reports whether key is within budget for the current window | Allow 报告 key 在当前窗口内是否未超预算.
func (l *Limiter) Allow(ctx context.Context, key string, max int) bool {
	if max <= 0 {
		return true
	}
	if l.redis != nil {
		allowed, err := l.redisAllow(ctx, key, max)
		if err == nil {
			return allowed
		}
	}
	return l.local.allow(key, l.window, max, l.now())
}

func (l *Limiter) redisAllow(ctx context.Context, key string, max int) (bool, error) {
	redisKey := l.keyPrefix + key
	nowMs := l.now().UnixMilli()
	windowMs := l.window.Milliseconds()
	// A timestamp alone is not unique: a single instance can handle many
	// requests in one millisecond. The per-limiter ID and sequence prevent ZADD
	// from overwriting an earlier hit with the same score.
	// 仅时间戳不唯一：单个实例在一毫秒内可能处理多个请求。每个 limiter 的 ID
	// 与序列号防止 ZADD 以相同分值覆盖先前的请求记录。
	member := fmt.Sprintf("%s:%d", l.memberID, l.sequence.Add(1))

	result, err := rateLimitScript.Run(ctx, l.redis,
		[]string{redisKey},
		nowMs, windowMs, max, member,
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// StartCleanup runs periodic eviction for the local fallback map. The fallback
// remains available even when Redis is configured, so cleanup is useful in
// either mode. It must be called: without it the local fallback map only ever
// grows, and every key ever seen (per IP / per user when limiting that way)
// keeps an entry forever. The typical pattern is go l.StartCleanup(stop) at
// startup and close(stop) before the process exits.
// StartCleanup 对本地回退 map 做周期性清理。即使配置了 Redis，回退依然可用，因此两种
// 模式下清理都有意义。必须调用：不调用时本地回退的 map 只增不减，每个出现过的 key（按
// IP、用户限流时就是每个 IP、每个用户）都会永久占用一个条目。典型用法是启动时
// go l.StartCleanup(stop)，进程退出前 close(stop)。
func (l *Limiter) StartCleanup(stopCh <-chan struct{}) {
	l.local.startCleanup(l.window, stopCh)
}

type localEntry struct {
	mu         sync.Mutex
	timestamps []time.Time
	deleted    bool
}

type localLimiter struct {
	entries sync.Map // key -> *localEntry | 键 -> *localEntry
}

func newLocalLimiter() *localLimiter {
	return &localLimiter{}
}

func (l *localLimiter) allow(key string, window time.Duration, max int, now time.Time) bool {
	cutoff := now.Add(-window)

	for {
		val, _ := l.entries.LoadOrStore(key, &localEntry{})
		entry := val.(*localEntry)

		entry.mu.Lock()
		if entry.deleted {
			entry.mu.Unlock()
			continue
		}

		valid := entry.timestamps[:0]
		for _, t := range entry.timestamps {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		entry.timestamps = valid

		if len(entry.timestamps) >= max {
			entry.mu.Unlock()
			return false
		}
		entry.timestamps = append(entry.timestamps, now)
		entry.mu.Unlock()
		return true
	}
}

func (l *localLimiter) startCleanup(window time.Duration, stopCh <-chan struct{}) {
	if window <= 0 {
		window = time.Minute
	}
	ticker := time.NewTicker(localCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.cleanupExpired(window, time.Now())
		case <-stopCh:
			return
		}
	}
}

func (l *localLimiter) cleanupExpired(window time.Duration, now time.Time) {
	// Evict against the limiter's own window, not a hardcoded minute, so
	// non-minute limiters (e.g. a per-day cap) are not dropped early.
	// 按 limiter 自身的窗口清理，而不是写死一分钟，避免非分钟级 limiter（如按天限额）
	// 被过早清除。
	cutoff := now.Add(-window)
	l.entries.Range(func(key, val any) bool {
		entry := val.(*localEntry)
		entry.mu.Lock()
		allExpired := true
		for _, t := range entry.timestamps {
			if t.After(cutoff) {
				allExpired = false
				break
			}
		}
		if allExpired {
			entry.deleted = true
			// Delete only if the map still points to this entry. An unconditional
			// delete can remove a replacement installed by another goroutine.
			// 仅当 map 仍指向该条目时才删除。无条件删除可能误删其他 goroutine
			// 刚放入的替换条目。
			l.entries.CompareAndDelete(key, entry)
		}
		entry.mu.Unlock()
		return true
	})
}
