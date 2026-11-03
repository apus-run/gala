package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Session aliases the driver session type | Session 是驱动 session 类型的别名。
type Session = mongo.Session

// SessionContext aliases the driver session context type
// | SessionContext 是驱动 session context 类型的别名。
type SessionContext = mongo.SessionContext

type session struct {
	mongo.Session
	processor processor
	logMode   bool
}

var _ mongo.Session = (*session)(nil)

// SetLogMode turns command logging on or off | 开启或关闭命令日志.
func (ws *session) SetLogMode(logMode bool) {
	ws.logMode = logMode
}

// EndSession implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) EndSession(ctx context.Context) {
	_ = ws.processor(func(c *cmd) error {
		ws.Session.EndSession(ctx)
		logCmd(ws.logMode, c, "EndSession", nil)
		return nil
	})
}

// StartTransaction implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) StartTransaction(topts ...*options.TransactionOptions) error {
	return ws.processor(func(c *cmd) error {
		logCmd(ws.logMode, c, "StartTransaction", nil)
		return ws.Session.StartTransaction(topts...)
	})
}

// AbortTransaction implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) AbortTransaction(ctx context.Context) error {
	return ws.processor(func(c *cmd) error {
		logCmd(ws.logMode, c, "AbortTransaction", nil)
		return ws.Session.AbortTransaction(ctx)
	})
}

// CommitTransaction implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) CommitTransaction(ctx context.Context) error {
	return ws.processor(func(c *cmd) error {
		logCmd(ws.logMode, c, "CommitTransaction", nil)
		return ws.Session.CommitTransaction(ctx)
	})
}

// ClusterTime implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) ClusterTime() (raw bson.Raw) {
	_ = ws.processor(func(c *cmd) error {
		raw = ws.Session.ClusterTime()
		logCmd(ws.logMode, c, "ClusterTime", raw)
		return nil
	})
	return
}

// AdvanceClusterTime implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) AdvanceClusterTime(br bson.Raw) error {
	return ws.processor(func(c *cmd) error {
		logCmd(ws.logMode, c, "AdvanceClusterTime", nil)
		return ws.Session.AdvanceClusterTime(br)
	})
}

// OperationTime implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) OperationTime() (ts *primitive.Timestamp) {
	_ = ws.processor(func(c *cmd) error {
		ts = ws.Session.OperationTime()
		logCmd(ws.logMode, c, "OperationTime", ts)
		return nil
	})
	return
}

// AdvanceOperationTime implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) AdvanceOperationTime(pt *primitive.Timestamp) error {
	return ws.processor(func(c *cmd) error {
		logCmd(ws.logMode, c, "AdvanceOperationTime", nil)
		return ws.Session.AdvanceOperationTime(pt)
	})
}

// WithTransaction implements mongo.Session, logging the command | 实现 mongo.Session 并记录命令日志.
func (ws *session) WithTransaction(ctx context.Context, fn func(sessCtx SessionContext) (any, error),
	opts ...*options.TransactionOptions) (out any, err error) {
	err = ws.processor(func(c *cmd) error {
		logCmd(ws.logMode, c, "WithTransaction", nil)
		out, err = ws.Session.WithTransaction(ctx, fn, opts...)
		return err
	})
	return
}
