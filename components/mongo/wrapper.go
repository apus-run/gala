package mongo

import (
	"context"
	"fmt"
)

// Mongo bundles a Client, Database and Collection created from one Config
// | Mongo 组装由同一份 Config 创建的 Client、Database 与 Collection。
type Mongo struct {
	*Client
	*Database
	*Collection
}

// Open connects with opts and bundles the configured database and collection;
// Debug enables logging and the interceptor chain.
// Open 按 opts 建连并组装配置的数据库与集合；Debug 开启日志与拦截器链。
func Open(ctx context.Context, opts ...Option) (*Mongo, error) {
	cfg := NewOptions(opts...)
	client, err := client(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new client fail: %v", err)
	}

	if cfg.Debug {
		client.SetLogMode(true)
		client.WrapProcessor(InterceptorChain(cfg.interceptors...))
	}

	db := client.Database(cfg.DatabaseName)
	coll := db.Collection(cfg.CollectionName)

	if cfg.Debug {
		coll.SetLogMode(cfg.Debug)
	}

	return &Mongo{
		Client:     client,
		Database:   db,
		Collection: coll,
	}, nil
}
