// Package rdbtest provides an in-memory redis for tests.
package rdbtest

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/apus-run/gala/components/rdb"
)

// New starts a miniredis server and returns a client connected to it.
// Both are closed when the test finishes.
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
