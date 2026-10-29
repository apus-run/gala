package mongo

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	driver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func writerClient(t *testing.T) *driver.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := driver.Connect(ctx, options.Client().ApplyURI("mongodb://root:example@localhost:27017/"))
	if err == nil {
		err = cli.Ping(ctx, nil)
	}
	if err != nil {
		t.Skipf("mongodb not available: %v", err)
	}
	t.Cleanup(func() { _ = cli.Disconnect(context.Background()) })
	return cli
}

// Flush 失败时数据必须还在 buffer 里，下一次 Flush 才能把它写出去。
func TestBufferedWriterFlushKeepsBufferOnError(t *testing.T) {
	cli := writerClient(t)
	coll := "writer_flush_retry"
	cli.Database("test_db").Collection(coll).Drop(context.Background())

	bw := NewBufferedWriter(cli, "test_db", coll, 10)
	// 同一个 _id 写两次：InsertMany 会因重复键失败
	_ = bw.Write(bson.M{"_id": 1, "v": "a"})
	_ = bw.Write(bson.M{"_id": 1, "v": "b"})
	if err := bw.Flush(); err == nil {
		t.Fatal("Flush should fail on duplicate key")
	}
	if len(bw.buffer) != 2 {
		t.Fatalf("buffer len = %d after failed Flush, want 2 (records must not be dropped)", len(bw.buffer))
	}
}

func TestBufferedWriterFlushClearsBufferOnSuccess(t *testing.T) {
	cli := writerClient(t)
	coll := "writer_flush_ok"
	cli.Database("test_db").Collection(coll).Drop(context.Background())

	bw := NewBufferedWriter(cli, "test_db", coll, 10)
	_ = bw.Write(bson.M{"_id": 1})
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(bw.buffer) != 0 {
		t.Fatalf("buffer len = %d after successful Flush, want 0", len(bw.buffer))
	}
	n, _ := cli.Database("test_db").Collection(coll).CountDocuments(context.Background(), bson.M{})
	if n != 1 {
		t.Fatalf("stored %d docs, want 1", n)
	}
}
