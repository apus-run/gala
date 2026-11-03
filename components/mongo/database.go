package mongo

import (
	"context"
	"sync"

	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Database is a handle to a MongoDB database | Database 是 MongoDB 数据库的句柄。
type Database struct {
	mu       sync.Mutex
	database *mongo.Database

	registry *bsoncodec.Registry

	processor processor
	logMode   bool
}

// SetLogMode toggles command logging on this database | SetLogMode 开关该 database 的命令日志。
func (d *Database) SetLogMode(logMode bool) {
	d.logMode = logMode
}

// Client returns the client this database belongs to | Client 返回该 database 所属的 client。
func (d *Database) Client() *Client {
	d.mu.Lock()
	defer d.mu.Unlock()

	cc := d.database.Client()
	if cc == nil {
		return nil
	}

	return &Client{
		client:   cc,
		registry: d.registry,
	}
}

// RunCommand runs a command against the database | RunCommand 对该数据库执行命令。
func (d *Database) RunCommand(ctx context.Context, runCommand any, opts ...*options.RunCmdOptions) *SingleResult {
	option := options.RunCmd()
	if len(opts) > 0 && opts != nil {
		option = opts[0]
	}
	return d.database.RunCommand(ctx, runCommand, option)
}

// RunCommandCursor runs a command returning a cursor | RunCommandCursor 执行返回 cursor 的命令。
func (d *Database) RunCommandCursor(ctx context.Context, cmd any) *Cursor {
	cur, err := d.database.RunCommandCursor(ctx, cmd)
	return &Cursor{
		ctx:    ctx,
		cursor: cur,
		err:    err,
	}
}

// ListCollections lists collections matching filter | ListCollections 列出匹配 filter 的集合。
func (d *Database) ListCollections(ctx context.Context, filter any, opts ...*options.ListCollectionsOptions) (
	cur *mongo.Cursor, err error) {
	err = d.processor(func(cmd *cmd) error {
		cur, err = d.database.ListCollections(ctx, filter, opts...)
		logCmd(d.logMode, cmd, "ListCollections", cur, filter)
		return err
	})
	return
}

// Collection gets a collection from the database | Collection 从数据库获取集合。
func (d *Database) Collection(name string, opts ...*options.CollectionOptions) *Collection {
	if d.database == nil {
		return nil
	}
	coll := d.database.Collection(name, opts...)
	if coll == nil {
		return nil
	}
	return &Collection{
		collection: coll,
		registry:   d.registry,

		processor: d.processor,
		logMode:   d.logMode,
	}
}

// CreateCollection creates a collection and returns its handle
// | CreateCollection 创建集合并返回其句柄。
func (d *Database) CreateCollection(ctx context.Context, name string, opts *options.CollectionOptions) (*Collection, error) {
	if err := d.database.CreateCollection(ctx, name); err != nil {
		return nil, err
	}
	cp := d.database.Collection(name, opts)
	return &Collection{
		collection: cp,
		registry:   d.registry,
	}, nil
}

// GetDatabaseName returns the name of the database | GetDatabaseName 返回数据库名。
func (d *Database) GetDatabaseName() string {
	return d.database.Name()
}

// GetReadConcern returns the database read concern | GetReadConcern 返回数据库的 read concern。
func (d *Database) GetReadConcern() *readconcern.ReadConcern { return d.database.ReadConcern() }

// GetReadPreference returns the database read preference | GetReadPreference 返回数据库的读偏好。
func (d *Database) GetReadPreference() *readpref.ReadPref { return d.database.ReadPreference() }

// DropDatabase drops the database | DropDatabase 删除数据库。
func (d *Database) DropDatabase(ctx context.Context) error {
	return d.processor(func(c *cmd) error {
		logCmd(d.logMode, c, "Drop", nil)
		return d.database.Drop(ctx)
	})
}

// GetDatabase returns the underlying official database handle | GetDatabase 返回底层官方 database 句柄。
func (d *Database) GetDatabase() *mongo.Database {
	return d.database
}

// WriteConcern returns the database write concern | WriteConcern 返回数据库的 write concern。
func (d *Database) WriteConcern() (res *writeconcern.WriteConcern) {
	_ = d.processor(func(c *cmd) error {
		res = d.database.WriteConcern()
		logCmd(d.logMode, c, "WriteConcern", res)
		return nil
	})
	return
}
