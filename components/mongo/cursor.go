package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// Cursor wraps a mongo.Cursor, decoding documents as they are iterated
// | Cursor 包装 mongo.Cursor，在迭代时解码文档。
type Cursor struct {
	ctx    context.Context
	cursor *mongo.Cursor
	err    error
}

// Next gets the next document for this cursor and decodes it into result. It
// returns true if there were no errors and the cursor has not been exhausted.
// Next 取下一条文档并解码进 result。无错误且游标未耗尽时返回 true。
func (c *Cursor) Next(result any) bool {
	if c.err != nil {
		return false
	}
	if c.cursor.Next(c.ctx) {
		err := c.cursor.Decode(result)
		if err != nil {
			c.err = err
			return false
		}
		return true
	}
	return false
}

// All iterates the cursor and decodes each document into results. The results
// parameter must be a pointer to a slice. Prefer All() inside Query or
// Aggregate.
// All 遍历游标并把每条文档解码进 results。results 必须是切片指针。
// 在 Query 或 Aggregate 中推荐使用 All()。
func (c *Cursor) All(results any) error {
	if c.err != nil {
		return c.err
	}
	return c.cursor.All(c.ctx, results)
}

// ID returns the ID of this cursor, or 0 if the cursor has been closed or exhausted.
//func (c *Cursor) ID() int64 {
//	if c.err != nil {
//		return 0
//	}
//	return c.cursor.ID()
//}

// Close closes this cursor. Next and TryNext must not be called after Close.
// When the cursor is no longer in use it should be closed explicitly.
// Close 关闭游标。Close 之后不得再调用 Next 与 TryNext。
// 游标不再使用时应主动关闭。
func (c *Cursor) Close() error {
	if c.err != nil {
		return c.err
	}
	return c.cursor.Close(c.ctx)
}

// Err returns the cursor's last error, or nil if none occurred
// | Err 返回游标的最后一个错误，无错误时返回 nil。
func (c *Cursor) Err() error {
	if c.err != nil {
		return c.err
	}
	return c.cursor.Err()
}
