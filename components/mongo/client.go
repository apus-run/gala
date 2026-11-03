package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Client is a handle wrapping the official MongoDB client | Client 包装官方 MongoDB 客户端的句柄。
type Client struct {
	client *mongo.Client

	registry *bsoncodec.Registry

	processor processor
	logMode   bool
}

func defaultProcessor(processFn processFn) error {
	return processFn(&cmd{req: make([]any, 0, 1)})
}

// NewClient creates a MongoDB client from options | NewClient 按选项创建 MongoDB 客户端。
func NewClient(ctx context.Context, opts ...Option) (*Client, error) {
	cfg := NewOptions(opts...)
	return client(ctx, cfg)
}

func client(ctx context.Context, cfg *Config) (*Client, error) {
	option := options.Client()
	option = cfg.ClientOptions
	option.ApplyURI(cfg.Uri)

	client, err := mongo.Connect(ctx, option)
	if err != nil {
		return nil, fmt.Errorf("can't connect to mongo: %w", err)
	}

	// Half of the default connect timeout
	// 取默认连接超时的一半
	pCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = client.Ping(pCtx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("can't ping mongo: %w", err)
	}

	return &Client{
		client:    client,
		registry:  option.Registry,
		processor: defaultProcessor,
	}, nil
}

// SetLogMode toggles command logging on this client | SetLogMode 开关该 client 的命令日志。
func (c *Client) SetLogMode(logMode bool) {
	c.logMode = logMode
}

// WrapProcessor wraps the command processor, e.g. with InterceptorChain
// | WrapProcessor 包装命令处理器，例如接上 InterceptorChain。
func (c *Client) WrapProcessor(wrapFn func(processFn) processFn) {
	c.processor = func(fn processFn) error {
		return wrapFn(fn)(&cmd{req: make([]any, 0, 1)})
	}
}

// Disconnect closes sockets to the topology referenced by this Client.
// Disconnect 关闭该 Client 连接的拓扑上的所有 socket。
func (c *Client) Disconnect(ctx context.Context) error {
	return c.processor(func(cmd *cmd) error {
		logCmd(c.logMode, cmd, "Disconnect", nil)
		return c.client.Disconnect(ctx)
	})
}

// Close closes sockets to the topology referenced by this Client.
// Close 关闭该 Client 连接的拓扑上的所有 socket。
func (c *Client) Close(ctx context.Context) error {
	err := c.client.Disconnect(ctx)
	return err
}

// Ping confirms the connection is alive | Ping 确认连接仍存活。
func (c *Client) Ping(timeout int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	return c.processor(func(cmd *cmd) error {
		logCmd(c.logMode, cmd, "Ping", nil)
		return c.client.Ping(ctx, readpref.Primary())
	})
}

// Database returns a handle to the named database | Database 返回指定数据库的句柄。
func (c *Client) Database(name string, dbOpts ...*options.DatabaseOptions) *Database {
	var db *mongo.Database
	c.processor(func(cmd *cmd) error {
		db = c.client.Database(name, dbOpts...)
		cmd.dbName = name
		logCmd(c.logMode, cmd, "Database", db, name)
		return nil
	})
	if db == nil {
		return nil
	}
	return &Database{
		database: db,
		registry: c.registry,

		processor: c.processor,
		logMode:   c.logMode,
	}
}

// ServerVersion returns the MongoDB server version, like 4.4.0; an empty
// string on failure.
// ServerVersion 返回 MongoDB 服务器版本（如 4.4.0）；失败时返回空字符串。
func (c *Client) ServerVersion() string {
	var buildInfo bson.Raw
	err := c.client.Database("admin").RunCommand(
		context.Background(),
		bson.D{{"buildInfo", 1}},
	).Decode(&buildInfo)
	if err != nil {
		fmt.Println("run command err", err)
		return ""
	}
	v, err := buildInfo.LookupErr("version")
	if err != nil {
		fmt.Println("look up err", err)
		return ""
	}
	return v.StringValue()
}

// Client returns the underlying official client; commands issued on it
// bypass logging and interceptors.
// Client 返回底层官方客户端；在它上面执行的命令不经过日志与拦截器。
func (c *Client) Client() *mongo.Client { return c.client }

// StartSession starts a new session | StartSession 开启一个新会话。
func (c *Client) StartSession(opts ...*options.SessionOptions) (ss Session, err error) {
	var ms mongo.Session
	err = c.processor(func(cmd *cmd) error {
		var serr error
		ms, serr = c.client.StartSession(opts...)
		logCmd(c.logMode, cmd, "StartSession", ms)
		return serr
	})
	if err != nil {
		return nil, err
	}
	return &session{Session: ms, logMode: c.logMode, processor: c.processor}, nil
}

// UseSession runs fn inside a session | UseSession 在会话中执行 fn。
func (c *Client) UseSession(ctx context.Context, fn func(SessionContext) error) error {
	return c.processor(func(cmd *cmd) error {
		logCmd(c.logMode, cmd, "UseSession", nil)
		return c.client.UseSession(ctx, fn)
	})
}

// UseSessionWithOptions runs fn inside a session with custom options
// | UseSessionWithOptions 在带选项的会话中执行 fn。
func (c *Client) UseSessionWithOptions(ctx context.Context, opts *options.SessionOptions, fn func(SessionContext) error) error {
	return c.processor(func(cmd *cmd) error {
		logCmd(c.logMode, cmd, "UseSessionWithOptions", nil)
		return c.client.UseSessionWithOptions(ctx, opts, fn)
	})
}

// ListDatabaseNames lists database names matching filter | ListDatabaseNames 列出匹配 filter 的数据库名。
func (c *Client) ListDatabaseNames(ctx context.Context, filter any, opts ...*options.ListDatabasesOptions) (
	dbs []string, err error) {

	err = c.processor(func(cmd *cmd) error {
		dbs, err = c.client.ListDatabaseNames(ctx, filter, opts...)
		logCmd(c.logMode, cmd, "ListDatabaseNames", dbs, filter)
		return err
	})
	return
}

// ListDatabases lists databases matching filter | ListDatabases 列出匹配 filter 的数据库。
func (c *Client) ListDatabases(ctx context.Context, filter any, opts ...*options.ListDatabasesOptions) (
	dbr mongo.ListDatabasesResult, err error) {

	err = c.processor(func(cmd *cmd) error {
		dbr, err = c.client.ListDatabases(ctx, filter, opts...)
		logCmd(c.logMode, cmd, "ListDatabases", dbr, filter)
		return err
	})
	return
}

// WithSession runs fn inside the given session, mirroring mongo.WithSession
// | WithSession 在给定会话中执行 fn，对应 mongo.WithSession。
func WithSession(ctx context.Context, sess Session, fn func(SessionContext) error) error {
	return mongo.WithSession(ctx, sess, fn)
}
