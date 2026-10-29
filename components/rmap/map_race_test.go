package rmap

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// SetAndWait 的超时与匹配的通知同时到达时不能向已关闭的 channel 发送（panic）。
func TestSetAndWaitTimeoutRacingNotificationDoesNotPanic(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:16379", Password: redisPwd})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	ctx := context.Background()
	m, err := Join(ctx, "race-test", rdb)
	require.NoError(t, err)
	defer cleanup(t, m)

	var wg sync.WaitGroup
	for i := range 300 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 超时设得很短，让取消与通知到达互相竞争
			cctx, cancel := context.WithTimeout(ctx, time.Duration(i%5+1)*time.Millisecond)
			defer cancel()
			_, _ = m.SetAndWait(cctx, "k", "v")
		}()
	}
	wg.Wait()
}

// Close 必须在有等待者时也能返回。
func TestCloseReturnsWithPendingWaiter(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:16379", Password: redisPwd})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	ctx := context.Background()
	m, err := Join(ctx, "close-test", rdb)
	require.NoError(t, err)

	go func() { _, _ = m.SetAndWait(ctx, "k", "v") }()
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked forever")
	}
}
