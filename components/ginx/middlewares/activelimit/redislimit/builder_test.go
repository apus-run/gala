package redislimit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func testRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	server.SetTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func request(router http.Handler) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	return response
}

func TestRedisActiveLimitSharesCapacityAndReleases(t *testing.T) {
	_, client := testRedis(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	first, second := gin.New(), gin.New()
	first.Use(NewRedisActiveLimit(client, 1, "shared").Build())
	second.Use(NewRedisActiveLimit(client, 1, "shared").Build())
	first.GET("/", func(c *gin.Context) { close(entered); <-release; c.Status(204) })
	second.GET("/", func(c *gin.Context) { c.Status(204) })
	done := make(chan int, 1)
	go func() { done <- request(first).Code }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first request not admitted")
	}
	require.Equal(t, 429, request(second).Code)
	require.Equal(t, int64(1), client.ZCard(context.Background(), "shared:leases").Val())
	once.Do(func() { close(release) })
	require.Equal(t, 204, <-done)
	require.Equal(t, 204, request(second).Code)
	require.Equal(t, int64(0), client.ZCard(context.Background(), "shared:leases").Val())
}

type evalFunc func(context.Context, string, []string, ...any) *redis.Cmd

func (f evalFunc) Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
	return f(ctx, script, keys, args...)
}

func TestRedisActiveLimitFailedReleaseExpires(t *testing.T) {
	server, client := testRedis(t)
	failed := false
	commands := evalFunc(func(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
		if script == releaseLease && !failed {
			failed = true
			return redis.NewCmdResult(nil, errors.New("release unavailable"))
		}
		return client.Eval(ctx, script, keys, args...)
	})
	router := gin.New()
	router.Use(NewRedisActiveLimit(commands, 1, "expiry").SetLeaseTTL(time.Minute).SetLogFunc(nil).Build())
	router.GET("/", func(c *gin.Context) { c.Status(204) })
	require.Equal(t, 204, request(router).Code)
	require.Equal(t, 429, request(router).Code)
	server.FastForward(time.Minute)
	server.SetTime(time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC))
	require.Equal(t, 204, request(router).Code)
	require.False(t, server.Exists("expiry:leases"))
}

func TestRedisActiveLimitCleansUpAfterCancellationAndPanic(t *testing.T) {
	for _, panicHandler := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "panic"}[panicHandler], func(t *testing.T) {
			_, client := testRedis(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			released := false
			commands := evalFunc(func(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
				if script == releaseLease {
					released = true
					require.NoError(t, ctx.Err())
					_, bounded := ctx.Deadline()
					require.True(t, bounded)
				}
				return client.Eval(ctx, script, keys, args...)
			})
			router := gin.New()
			router.Use(gin.RecoveryWithWriter(io.Discard), NewRedisActiveLimit(commands, 1, "cleanup").Build())
			router.GET("/", func(c *gin.Context) {
				cancel()
				if panicHandler {
					panic("handler failed")
				}
				c.Status(204)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
			require.True(t, released)
			require.Equal(t, int64(0), client.ZCard(context.Background(), "cleanup:leases").Val())
			if panicHandler {
				require.Equal(t, 500, response.Code)
			} else {
				require.Equal(t, 204, response.Code)
			}
		})
	}
}

func TestRedisActiveLimitAmbiguousAcquisitionIsReleased(t *testing.T) {
	_, client := testRedis(t)
	commands := evalFunc(func(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
		result := client.Eval(ctx, script, keys, args...)
		if script == acquireLease {
			require.NoError(t, result.Err())
			return redis.NewCmdResult(nil, context.DeadlineExceeded)
		}
		return result
	})
	router := gin.New()
	router.Use(NewRedisActiveLimit(commands, 1, "lost-reply").SetLogFunc(nil).Build())
	router.GET("/", func(*gin.Context) { t.Fatal("failed acquisition reached handler") })
	require.Equal(t, 500, request(router).Code)
	require.Equal(t, int64(0), client.ZCard(context.Background(), "lost-reply:leases").Val())
}

func TestLeaseScriptsAreIdempotentAndExpireIndividualTokens(t *testing.T) {
	server, client := testRedis(t)
	ctx := context.Background()
	key := []string{"tokens:leases"}
	acquire := func(token string, ttl int64) int64 {
		result, err := client.Eval(ctx, acquireLease, key, token, 2, ttl).Int64()
		require.NoError(t, err)
		return result
	}
	require.Equal(t, int64(1), acquire("short", 1000))
	require.Equal(t, int64(1), acquire("short", 1000))
	require.Equal(t, int64(1), acquire("long", 10000))
	require.Equal(t, int64(0), acquire("third", 1000))
	require.Equal(t, 10*time.Second, server.TTL(key[0]))
	server.FastForward(time.Second)
	server.SetTime(time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC))
	require.Equal(t, int64(1), acquire("replacement", 1000))
	require.Equal(t, 9*time.Second, server.TTL(key[0]), "short leases must not expire longer leases")
	for range 2 {
		require.NoError(t, client.Eval(ctx, releaseLease, key, "short").Err())
	}
	require.Equal(t, int64(2), client.ZCard(ctx, key[0]).Val(), "stale release must not free another request")
}

func TestRedisActiveLimitBoundsExecutionAndRedisOperations(t *testing.T) {
	_, client := testRedis(t)
	commands := evalFunc(func(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), time.Second)
		return client.Eval(ctx, script, keys, args...)
	})
	router := gin.New()
	router.Use(NewRedisActiveLimit(commands, 1, "deadline").SetLeaseTTL(20 * time.Millisecond).Build())
	router.GET("/", func(c *gin.Context) { <-c.Request.Context().Done(); c.Status(504) })
	require.Equal(t, 504, request(router).Code)
	require.Equal(t, int64(0), client.ZCard(context.Background(), "deadline:leases").Val())
}

func TestRedisActiveLimitDynamicCapacityAndValidation(t *testing.T) {
	_, client := testRedis(t)
	limiter := NewRedisActiveLimit(client, 0, "limit")
	router := gin.New()
	router.Use(limiter.Build())
	router.GET("/", func(c *gin.Context) { c.Status(204) })
	require.Equal(t, 429, request(router).Code)
	limiter.SetMaxActive(1)
	require.Equal(t, 204, request(router).Code)
	require.Panics(t, func() { NewRedisActiveLimit(nil, 1, "limit") })
	require.Panics(t, func() { NewRedisActiveLimit(client, 1, "") })
	require.Panics(t, func() { limiter.SetLeaseTTL(0) })
	require.Panics(t, func() { limiter.SetOperationTimeout(0) })
}

func TestRedisActiveLimitRoundsTTLUpAndRejectsLateAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	released := false
	commands := evalFunc(func(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd {
		if script == acquireLease {
			require.Equal(t, int64(1001), args[2])
			cancel() // The request ended after acquisition but before its reply arrived.
		} else {
			released = true
			require.NoError(t, ctx.Err())
		}
		return redis.NewCmdResult(int64(1), nil)
	})
	router := gin.New()
	router.Use(NewRedisActiveLimit(commands, 1, "late").SetLeaseTTL(time.Second + time.Nanosecond).
		SetOperationTimeout(500 * time.Millisecond).Build())
	router.GET("/", func(*gin.Context) { t.Fatal("expired admission reached business handler") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	require.Equal(t, 504, response.Code)
	require.True(t, released)
}
