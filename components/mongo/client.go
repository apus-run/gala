package mongo

import (
	"context"
	"fmt"

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

// NewClient creates a MongoDB client from options. Construction performs no
// I/O: the driver dials lazily on the first command, so a bad URI or an
// unreachable server surfaces on Ping or on the first command, not here.
// Call Ping right after NewClient to fail fast at startup.
// NewClient 按选项创建 MongoDB 客户端。构造不做任何 I/O：驱动在首条命令时才
// 真正拨号，错误的 URI 或不可达的服务器要到 Ping 或首条命令时才暴露。若需
// 启动即失败，请在 NewClient 之后显式调用 Ping。
func NewClient(opts ...Option) (*Client, error) {
	cfg := NewOptions(opts...)
	return client(cfg)
}

func client(cfg *Config) (*Client, error) {
	option := cfg.ClientOptions
	option.ApplyURI(cfg.Uri)

	// mongo.Connect does not dial: it only validates options and defers
	// connecting to the first command, so context.Background() suffices.
	// mongo.Connect 不会拨号：它只校验选项，建连推迟到首条命令，
	// 因此这里传 context.Background() 即可。
	mc, err := mongo.Connect(context.Background(), option)
	if err != nil {
		return nil, fmt.Errorf("can't create mongo client: %w", err)
	}

	return &Client{
		client:    mc,
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

// Close closes sockets to the topology referenced by this Client; it is an
// alias of Disconnect and runs through the same processor chain.
// Close 关闭该 Client 连接的拓扑上的所有 socket；它是 Disconnect 的别名，
// 走同一条 processor 链。
func (c *Client) Close(ctx context.Context) error {
	return c.Disconnect(ctx)
}

// Ping confirms the connection is alive, respecting the caller's context
// | Ping 确认连接仍存活，遵循调用方的 context。
func (c *Client) Ping(ctx context.Context) error {
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
