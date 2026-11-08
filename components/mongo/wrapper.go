package mongo

import (
	"fmt"
)

// Mongo bundles a Client, Database and Collection created from one Config
// | Mongo 组装由同一份 Config 创建的 Client、Database 与 Collection。
type Mongo struct {
	*Client
	*Database
	*Collection
}

// Open builds a Client, Database and Collection from one Config without
// dialing; Debug enables logging and the interceptor chain. Use the embedded
// Client's Ping to fail fast on an unreachable server.
// Open 依据一份 Config 组装 Client、Database 与 Collection，不发起建连；
// Debug 开启日志与拦截器链。若需启动即发现不可达的服务器，请显式调用
// 内嵌 Client 的 Ping。
func Open(opts ...Option) (*Mongo, error) {
	cfg := NewOptions(opts...)
	client, err := client(cfg)
	if err != nil {
		return nil, fmt.Errorf("new client fail: %w", err)
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
