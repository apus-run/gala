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
}

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
	}
}

// NewLock builds a Locker backed by MongoDB | NewLock 构建基于 MongoDB 的 Locker。
func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, cli.collection, opts...)
}
