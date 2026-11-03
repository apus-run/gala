// Package rdbtest provides an in-memory redis for tests | rdbtest 为测试提供内存版 redis.
package rdbtest

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/apus-run/gala/components/rdb"
)

// New starts a miniredis server and returns a client connected to it.
// Both are closed when the test finishes.
// New 启动一个 miniredis 服务并返回连接它的客户端。两者都会在测试结束时关闭。
func New(tb testing.TB) rdb.Cmdable {
	tb.Helper()

	m, err := miniredis.Run()
	if err != nil {
		tb.Fatalf("rdbtest: start miniredis: %v", err)
	}
	tb.Cleanup(m.Close)

	p, err := rdb.New(&rdb.Options{Addr: m.Addr()})
	if err != nil {
		tb.Fatalf("rdbtest: new redis client: %v", err)
	}
	tb.Cleanup(func() { _ = p.Close() })

	return p.DB(context.Background())
}
