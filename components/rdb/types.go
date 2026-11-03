package rdb

import (
	"context"

	"github.com/redis/go-redis/v9"
)

var _ Provider = (*provider)(nil)

type (
	// Options is an alias of redis.Options | Options 是 redis.Options 的别名.
	Options = redis.Options
	// Client is an alias of redis.Client | Client 是 redis.Client 的别名.
	Client = redis.Client
	// UniversalClient is an alias of redis.UniversalClient | UniversalClient 是 redis.UniversalClient 的别名.
	UniversalClient = redis.UniversalClient
	// Tx is an alias of redis.Tx | Tx 是 redis.Tx 的别名.
	Tx = redis.Tx
	// Cmdable is an alias of redis.Cmdable | Cmdable 是 redis.Cmdable 的别名.
	Cmdable = redis.Cmdable
	// Pipeline is an alias of redis.Pipeline | Pipeline 是 redis.Pipeline 的别名.
	Pipeline = redis.Pipeline

	// StatusCmd is an alias of redis.StatusCmd | StatusCmd 是 redis.StatusCmd 的别名.
	StatusCmd = redis.StatusCmd
	// StringSliceCmd is an alias of redis.StringSliceCmd | StringSliceCmd 是 redis.StringSliceCmd 的别名.
	StringSliceCmd = redis.StringSliceCmd
	// BoolCmd is an alias of redis.BoolCmd | BoolCmd 是 redis.BoolCmd 的别名.
	BoolCmd = redis.BoolCmd
	// IntCmd is an alias of redis.IntCmd | IntCmd 是 redis.IntCmd 的别名.
	IntCmd = redis.IntCmd
	// FloatCmd is an alias of redis.FloatCmd | FloatCmd 是 redis.FloatCmd 的别名.
	FloatCmd = redis.FloatCmd
	// StringCmd is an alias of redis.StringCmd | StringCmd 是 redis.StringCmd 的别名.
	StringCmd = redis.StringCmd
	// Script is an alias of redis.Script | Script 是 redis.Script 的别名.
	Script = redis.Script
	// Pipeliner is an alias of redis.Pipeliner | Pipeliner 是 redis.Pipeliner 的别名.
	Pipeliner = redis.Pipeliner
)

// Provider hands out the redis client and owns its lifecycle | Provider 发放 redis 客户端并拥有其生命周期.
type Provider interface {
	// DB returns the shared redis client. It does not open a new session,
	// and ctx is currently unused.
	// DB 返回共享的 redis 客户端。它不会新建会话，ctx 目前未使用。
	DB(ctx context.Context) Cmdable
	// Close releases the underlying client. Closing twice is not an error.
	// Close 释放底层客户端。重复关闭不是错误。
	Close() error
}

type provider struct {
	cli redis.UniversalClient
}

// DB implements Provider | DB 实现 Provider 接口.
func (p *provider) DB(_ context.Context) Cmdable {
	return p.cli
}

// Close implements Provider | Close 实现 Provider 接口.
func (p *provider) Close() error {
	if err := p.cli.Close(); err != nil && !IsClosedError(err) {
		return err
	}
	return nil
}
