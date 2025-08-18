package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/apus-run/gala/components/dlock"
)

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
// NewClient creates a new MongoDB client.
func NewClient(cli *mongo.Client, collection *mongo.Collection) *Client {
	return &Client{
		client:     cli,
		collection: collection,
	}
}

func (cli *Client) NewLock(ctx context.Context, opts ...dlock.Option) (dlock.Locker, error) {
	return NewLock(ctx, cli.client, cli.collection, opts...)
}
