package rdb

import (
	"context"

	"github.com/redis/go-redis/v9"
)

var _ Provider = (*provider)(nil)

type (
	// Options is an alias of redis.Options.
	Options = redis.Options
	// Client is an alias of redis.Client.
	Client = redis.Client
	// UniversalClient is an alias of redis.UniversalClient.
	UniversalClient = redis.UniversalClient
	// Tx is an alias of redis.Tx.
	Tx = redis.Tx
	// Cmdable is an alias of redis.Cmdable.
	Cmdable = redis.Cmdable
	// Pipeline is an alias of redis.Pipeline.
	Pipeline = redis.Pipeline

	// StatusCmd is an alias of redis.StatusCmd.
	StatusCmd = redis.StatusCmd
	// StringSliceCmd is an alias of redis.StringSliceCmd.
	StringSliceCmd = redis.StringSliceCmd
	// BoolCmd is an alias of redis.BoolCmd.
	BoolCmd = redis.BoolCmd
	// IntCmd is an alias of redis.IntCmd.
	IntCmd = redis.IntCmd
	// FloatCmd is an alias of redis.FloatCmd.
	FloatCmd = redis.FloatCmd
	// StringCmd is an alias of redis.StringCmd.
	StringCmd = redis.StringCmd
	// Script is an alias of redis.Script.
	Script    = redis.Script
	Pipeliner = redis.Pipeliner
)

type Provider interface {
	// DB 创建一个新的数据库会话
	DB(ctx context.Context) Cmdable
}

type provider struct {
	cli *Client
}

// DB implements Provider.
func (p *provider) DB(_ context.Context) Cmdable {
	return p.cli
}
