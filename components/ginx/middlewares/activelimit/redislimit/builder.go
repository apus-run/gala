// Package redislimit bounds concurrent requests with expiring Redis leases.
package redislimit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/atomic"
)

type counter interface {
	Eval(context.Context, string, []string, ...any) *redis.Cmd
}

// Server time keeps expiry consistent across instances. Acquiring with the same
// token is idempotent, including when go-redis retries after losing a reply.
const acquireLease = `
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 1 end
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then return 0 end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[3]), ARGV[1])
local last = redis.call('ZREVRANGE', KEYS[1], 0, 0, 'WITHSCORES')
redis.call('PEXPIREAT', KEYS[1], math.ceil(tonumber(last[2])))
return 1
`

const releaseLease = `return redis.call('ZREM', KEYS[1], ARGV[1])`

// RedisActiveLimit shares admission across instances using one lease key.
// Configure setters before Build, except SetMaxActive, which is concurrency-safe.
// Do not copy an instance after use.
type RedisActiveLimit struct {
	maxActive        *atomic.Int64
	key              string
	cmd              counter
	logFn            func(msg any, args ...any)
	leaseTTL         time.Duration
	operationTimeout time.Duration
}

// NewRedisActiveLimit uses key + ":leases" for expiring per-request leases.
// cmd must support EVAL and honor context deadlines. For go-redis enable
// ContextTimeoutEnabled and configure bounded connection timeouts. Coordinate
// deployment when upgrading from INCR/DECR: the keys are independent.
func NewRedisActiveLimit(cmd counter, maxActive int64, key string) *RedisActiveLimit {
	if cmd == nil || key == "" {
		panic("redislimit: Redis client and key are required")
	}
	return &RedisActiveLimit{
		maxActive:        atomic.NewInt64(maxActive),
		key:              key + ":leases",
		cmd:              cmd,
		leaseTTL:         30 * time.Second,
		operationTimeout: time.Second,
		logFn:            func(msg any, args ...any) { fmt.Printf("%v: %v\n", msg, args) },
	}
}

// SetMaxActive changes the admission limit; zero or negative refuses new requests.
func (a *RedisActiveLimit) SetMaxActive(maxActive int64) *RedisActiveLimit {
	a.maxActive.Store(maxActive)
	return a
}

// SetLogFunc sets error logging; nil disables logging.
func (a *RedisActiveLimit) SetLogFunc(fn func(msg any, args ...any)) *RedisActiveLimit {
	if fn == nil {
		fn = func(any, ...any) {}
	}
	a.logFn = fn
	return a
}

// SetLeaseTTL sets both the request execution deadline and the lease lifetime.
// Handlers and downstream calls must honor Request.Context(); Go cannot forcibly
// stop a handler that ignores cancellation. TTL must be at least one millisecond.
func (a *RedisActiveLimit) SetLeaseTTL(ttl time.Duration) *RedisActiveLimit {
	if ttl < time.Millisecond {
		panic("redislimit: lease TTL must be at least one millisecond")
	}
	a.leaseTTL = ttl
	return a
}

// SetOperationTimeout sets the deadline for each Redis operation, including
// cleanup. The injected Redis client must honor context deadlines.
func (a *RedisActiveLimit) SetOperationTimeout(timeout time.Duration) *RedisActiveLimit {
	if timeout <= 0 {
		panic("redislimit: operation timeout must be positive")
	}
	a.operationTimeout = timeout
	return a
}

// Build snapshots configuration and returns a Gin middleware. A release uses
// its own bounded context so request cancellation does not prevent cleanup.
// A failed release or crashed process loses capacity only until lease expiry.
func (a *RedisActiveLimit) Build() gin.HandlerFunc {
	cmd, key, ttl, timeout, logFn := a.cmd, a.key, a.leaseTTL, a.operationTimeout, a.logFn
	ttlMillis := ttl.Milliseconds()
	if ttl%time.Millisecond != 0 {
		ttlMillis++ // Redis precision must never shorten the execution deadline.
	}
	return func(c *gin.Context) {
		limit := a.maxActive.Load()
		if limit <= 0 {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		request := c.Request
		ctx, cancel := context.WithTimeout(request.Context(), ttl)
		defer cancel()
		token := uuid.NewString()
		release := func() {
			cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer done()
			if err := cmd.Eval(cleanup, releaseLease, []string{key}, token).Err(); err != nil {
				logFn("redislimit: release lease", err)
			}
		}
		operation, done := context.WithTimeout(ctx, timeout)
		allowed, err := cmd.Eval(operation, acquireLease, []string{key}, token, limit, ttlMillis).Int64()
		done()
		if err != nil {
			// A timeout may mean the lease was created but its reply was lost.
			release()
			logFn("redislimit: acquire lease", err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if allowed != 1 {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		defer release()
		if ctx.Err() != nil {
			c.AbortWithStatus(http.StatusGatewayTimeout)
			return
		}
		c.Request = request.WithContext(ctx)
		defer func() { c.Request = request }()
		c.Next()
	}
}
