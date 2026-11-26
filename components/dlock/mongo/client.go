package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/apus-run/gala/components/dlock"
)

// Client creates distributed locks via MongoDB | Client 基于 MongoDB 创建分布式锁。
type Client struct {
	client     *mongo.Client
	collection *mongo.Collection
	// index is shared by every Locker this client creates, so the fail-closed
	// index check runs once per client instead of once per Run.
	// index 由该 client 创建的所有 Locker 共享，使 fail-closed 索引校验每个
	// client 只执行一次，而不是每个 Run 一次。
	index *indexCache
}

// Ensure Client implements dlock.Client and the optional dlock.Initializer |
// 确保 Client 实现 dlock.Client 以及可选的 dlock.Initializer。
var (
	_ dlock.Client      = (*Client)(nil)
	_ dlock.Initializer = (*Client)(nil)
)

/*
    client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}

	collection := client.Database(dbName).Collection("locks")
*/
// NewClient creates a new MongoDB client | NewClient 创建新的 MongoDB 客户端。
func NewClient(cli *mongo.Client, collection *mongo.Collection) *Client {
	return &Client{
		client:     cli,
		collection: collection,
		index:      &indexCache{},
	}
}

// NewLock builds a Locker backed by MongoDB. Index setup is not done here:
// call Init once before first use.
// NewLock 构建基于 MongoDB 的 Locker。这里不建索引：首次使用前调用一次 Init。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	locker, err := newLock(cli.collection, cli.index, opts...)
	if err != nil {
		// Return an untyped nil so callers never receive a non-nil interface
		// wrapping a nil *Lock.
		// 返回无类型的 nil，避免调用方拿到包着 nil *Lock 的非 nil 接口。
		return nil, err
	}
	return locker, nil
}

// Init creates the unique index on name the lock protocol relies on. Call it
// once during startup, before the first NewLock.
// Init 建立锁协议依赖的 name 唯一索引。启动时、首次 NewLock 前调用一次。
func (cli *Client) Init(ctx context.Context) error {
	return Init(ctx, cli.collection)
}
