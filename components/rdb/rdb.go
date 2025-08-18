package rdb

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewClient(opts *Options) (Provider, error) {
	cli := redis.NewClient(opts)
	return &provider{cli: cli}, nil
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
	if err != nil && errors.Is(err, redis.ErrClosed) {
		return err
	}

	return nil
}

func ConnectRDB(opts *Options) (Provider, error) {
	// 创建客户端
	cli := redis.NewClient(opts)

	// 创建超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := cli.Ping(ctx).Err(); err != nil {
		_ = Close(cli)
		return nil, err
	}
	return &provider{cli: cli}, nil
}

// NewScript returns a new Script instance.
func NewScript(script string) *Script {
	return redis.NewScript(script)
}
