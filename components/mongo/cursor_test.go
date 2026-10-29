package mongo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestCursor(t *testing.T) {
	type QueryTestItem struct {
		Id   primitive.ObjectID `bson:"_id"`
		Name string             `bson:"name"`
		Age  int                `bson:"age"`

		Instock []struct {
			Warehouse string `bson:"warehouse"`
			Qty       int    `bson:"qty"`
		} `bson:"instock"`
	}

	ast := require.New(t)
	cli := initClient()
	defer cli.Close(context.Background())
	defer cli.DropCollection(context.Background())

	id1 := primitive.NewObjectID()
	id2 := primitive.NewObjectID()
	id3 := primitive.NewObjectID()
	id4 := primitive.NewObjectID()
	docs := []any{
		bson.M{"_id": id1, "name": "Alice", "age": 18},
		bson.M{"_id": id2, "name": "Alice", "age": 19},
		bson.M{"_id": id3, "name": "Lucas", "age": 20},
		bson.M{"_id": id4, "name": "Lucas", "age": 21},
	}
	_, err := cli.InsertMany(context.Background(), docs)
	ast.NoError(err)

	var res QueryTestItem

	// "Alice" matches two records (id1, id2). Find sets no sort, so the order is
	// unspecified: check the set of ids, not their order.
	filter1 := bson.M{
		"name": "Alice",
	}

	cursor := cli.Find(context.Background(), filter1)
	ast.NoError(cursor.Err())

	seen := map[primitive.ObjectID]bool{}
	for i := 0; i < 2; i++ {
		ast.True(cursor.Next(&res))
		seen[res.Id] = true
	}
	ast.Equal(map[primitive.ObjectID]bool{id1: true, id2: true}, seen)

	val := cursor.Next(&res)
	ast.Equal(false, val)

	cursor.Close()

}
