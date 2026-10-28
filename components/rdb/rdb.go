// Package rdb is a thin wrapper around go-redis v9 that exposes the client
// through a Provider. Test helpers live in the rdbtest subpackage.
package rdb

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const connectTimeout = 3 * time.Second

// NewFromURL creates a Provider from a redis URL, e.g. redis://:pass@host:6379/0.
func NewFromURL(url string) (Provider, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis url: %w", err)
	}
	return New(opts)
}

// New creates a Provider from opts. It does not connect.
func New(opts *Options) (Provider, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}
	return &provider{cli: redis.NewClient(opts)}, nil
}

// NewFromClient wraps an existing client. The Provider takes over its
// lifecycle: Provider.Close closes cli.
func NewFromClient(cli *Client) (Provider, error) {
	if cli == nil {
		return nil, ErrNilClient
	}
	return &provider{cli: cli}, nil
}

// NewFromUniversalClient wraps a standalone, cluster or sentinel client.
// The Provider takes over its lifecycle: Provider.Close closes cli.
func NewFromUniversalClient(cli UniversalClient) (Provider, error) {
	if cli == nil {
		return nil, ErrNilClient
	}
	return &provider{cli: cli}, nil
}

// Unwrap returns the underlying *Client. It reports false when the Provider
// is not backed by a standalone *Client (a mock, or a cluster client).
func Unwrap(cli Provider) (*Client, bool) {
	if p, ok := cli.(*provider); ok {
		if c, ok := p.cli.(*Client); ok && c != nil {
			return c, true
		}
	}
	return nil, false
}

// Close closes rdb, ignoring a nil client and an already closed one.
func Close(rdb *Client) error {
	if rdb == nil {
		return nil
	}

	if err := rdb.Close(); err != nil && !IsClosedError(err) {
		return err
	}
	return nil
}

// Connect creates a Provider and verifies the connection with a PING.
// The PING is bounded by ctx and, if ctx has no earlier deadline, by a
// 3 second timeout.
func Connect(ctx context.Context, opts *Options) (Provider, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}
	cli := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := cli.Ping(ctx).Err(); err != nil {
		_ = Close(cli)
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}
	return &provider{cli: cli}, nil
}

// NewScript returns a new Script instance.
func NewScript(script string) *Script {
	return redis.NewScript(script)
}
