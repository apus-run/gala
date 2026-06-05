package rdb

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewClient(opts *Options) (Provider, error) {
	return NewRDBFromConfig(opts)
}

func NewRDB(url string) (Provider, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis url: %w", err)
	}
	return NewRDBFromConfig(opts)
}

func NewRDBFromConfig(opts *Options) (Provider, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}
	cli := redis.NewClient(opts)
	return &provider{cli: cli}, nil
}

func NewRDBFromClient(cli *Client) Provider {
	return &provider{cli: cli}
}

func Unwrap(cli Provider) (*Client, bool) {
	if p, ok := cli.(*provider); ok && p.cli != nil {
		return p.cli, true
	}
	return nil, false
}

func Close(rdb *Client) error {
	if rdb == nil {
		return nil
	}

	err := rdb.Close()
	if err != nil && !IsClosedError(err) {
		return err
	}

	return nil
}

func ConnectRDB(opts *Options) (Provider, error) {
	// 创建客户端
	rdb, err := NewRDBFromConfig(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}

	// 创建超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, ok := Unwrap(rdb)
	if !ok {
		return nil, fmt.Errorf("failed to unwrap redis client")
	}

	if err := client.Ping(ctx).Err(); err != nil {
		_ = Close(client)
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}
	return rdb, nil
}

// NewScript returns a new Script instance.
func NewScript(script string) *Script {
	return redis.NewScript(script)
}
