// Copyright 2017 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package gormadapter stores Casbin policies in a SQL database through GORM
// | 通过 GORM 将 Casbin 策略存入 SQL 数据库。
//
// # Notes | 说明
//
// This is casbin's gorm-adapter v3.41.0, maintained here for Casbin v3 with
// the whole-save transaction boundary and empty-policy-type validation
// fixed. Postgres, MySQL, SQL Server and pure-Go SQLite are supported.
// Constructors auto-migrate the casbin_rule table unless TurnOffAutoMigrate
// is used; with migration off the application must create the table itself.
// The caller owns an injected *gorm.DB; NewAdapter opens its own connection.
// 本包是 casbin gorm-adapter v3.41.0 的本地维护版，适配 Casbin v3，修复了
// 整份保存的事务边界与空策略类型校验。支持 Postgres、MySQL、SQL Server 与
// 纯 Go 的 SQLite。构造时默认自动迁移 casbin_rule 表，可用 TurnOffAutoMigrate
// 关闭；关闭后由应用自行建表。注入的 *gorm.DB 归调用方；NewAdapter 自行
// 建立连接。
//
// # Features | 功能
//
// - Adapter and BatchAdapter for casbin.persist | 实现 casbin.persist 的 Adapter 与 BatchAdapter
// - Context-aware load/save plus batch and filtered operations | 带上下文的加载/保存，以及批量与过滤操作
// - Transaction and BeginTransaction for atomic policy writes | Transaction 与 BeginTransaction 支持原子策略写入
//
// # Example | 示例
//
//	a, err := NewAdapterByDB(db)
//	e, err := casbin.NewSyncedEnforcer("model.conf", a)
package gormadapter

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/glebarez/sqlite"
	"github.com/pkg/errors"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"
)

const (
	defaultDatabaseName = "casbin"
	defaultTableName    = "casbin_rule"
)

const disableMigrateKey = "disableMigrateKey"
const customTableKey = "customTableKey"

// CasbinRule is one row of the policy table | CasbinRule 是策略表的一行.
type CasbinRule struct {
	// ID is the auto-increment primary key | 自增主键.
	ID uint `gorm:"primaryKey;autoIncrement"`
	// Ptype is the policy type, e.g. p or g | 策略类型，如 p 或 g.
	Ptype string `gorm:"size:100"`
	// V0 through V5 are the rule values | 规则值 v0–v5.
	V0 string `gorm:"size:100"`
	// V1 is rule value 1 | 规则值 v1。
	V1 string `gorm:"size:100"`
	// V2 is rule value 2 | 规则值 v2。
	V2 string `gorm:"size:100"`
	// V3 is rule value 3 | 规则值 v3。
	V3 string `gorm:"size:100"`
	// V4 is rule value 4 | 规则值 v4。
	V4 string `gorm:"size:100"`
	// V5 is rule value 5 | 规则值 v5。
	V5 string `gorm:"size:100"`
}

// TableName returns the backing table name | TableName 返回对应的表名.
func (CasbinRule) TableName() string {
	return "casbin_rule"
}

// Filter filters rules by ptype and value match | 按 ptype 与值匹配过滤规则.
type Filter struct {
	// Ptype lists matching policy types | 匹配的策略类型列表.
	Ptype []string
	// V0 through V5 list matching values | 匹配的值列表 v0–v5.
	V0 []string
	// V1 lists matching values for v1 | v1 的匹配值列表。
	V1 []string
	// V2 lists matching values for v2 | v2 的匹配值列表。
	V2 []string
	// V3 lists matching values for v3 | v3 的匹配值列表。
	V3 []string
	// V4 lists matching values for v4 | v4 的匹配值列表。
	V4 []string
	// V5 lists matching values for v5 | v5 的匹配值列表。
	V5 []string
}

// BatchFilter is a set of Filters applied in one filtered load | 一组 Filter，用于一次批量过滤加载.
type BatchFilter struct {
	filters []Filter
}

// Adapter represents the Gorm adapter for policy storage
// | Adapter 是基于 GORM 的策略存储适配器.
type Adapter struct {
	driverName     string
	dataSourceName string
	databaseName   string
	tablePrefix    string
	tableName      string
	dbSpecified    bool
	db             *gorm.DB
	isFiltered     bool
	transactionMu  *sync.Mutex
	muInitialize   sync.Once
}

var (
	_ persist.Adapter      = (*Adapter)(nil)
	_ persist.BatchAdapter = (*Adapter)(nil)
)

// finalizer is the destructor for Adapter | finalizer 是 Adapter 的析构函数.
func finalizer(a *Adapter) {
	sqlDB, err := a.db.DB()
	if err != nil {
		panic(err)
	}
	err = sqlDB.Close()
	if err != nil {
		panic(err)
	}
}

// specificPolicy selects a connection by table name (name-index kept in a map)
// | specificPolicy 按表名选连接（map 里存名字到下标）.
type specificPolicy int

// Resolve picks the connection pool recorded in the policy | Resolve 返回 policy 记录的连接池.
func (p *specificPolicy) Resolve(connPools []gorm.ConnPool) gorm.ConnPool {
	return connPools[*p]
}

// DbPool routes multiple databases to one gorm.DB via dbresolver
// | DbPool 通过 dbresolver 把多个数据库路由到一个 gorm.DB.
type DbPool struct {
	dbMap  map[string]specificPolicy
	policy *specificPolicy
	source *gorm.DB
}

func (dbPool *DbPool) switchDb(dbName string) *gorm.DB {
	*dbPool.policy = dbPool.dbMap[dbName]
	return dbPool.source.Clauses(dbresolver.Write)
}

// NewAdapter is the constructor for Adapter, opening its own connection.
// Params: databaseName, tableName, dbSpecified —
//
//	databaseName,{tableName/dbSpecified}
//	{database/dbSpecified}
//
// databaseName and tableName are user defined; their default values are
// "casbin" and "casbin_rule". dbSpecified is an optional bool parameter
// (default false) that says whether dataSourceName already points at an
// existing DB: with true you must ensure the DB in dataSourceName exists;
// with false the adapter automatically creates a DB named databaseName.
// NewAdapter 是 Adapter 的构造函数，自行建立连接。参数为 databaseName、
// tableName、dbSpecified——databaseName 与 tableName 由用户定义，默认
// "casbin" 与 "casbin_rule"。dbSpecified 是可选的 bool 参数（默认 false），
// 表示 dataSourceName 是否已指向既有数据库：为 true 时须确保该库存在，
// 为 false 时适配器会自动创建名为 databaseName 的数据库。
func NewAdapter(driverName string, dataSourceName string, params ...interface{}) (*Adapter, error) {
	a := &Adapter{}
	a.driverName = driverName
	a.dataSourceName = dataSourceName

	a.tableName = defaultTableName
	a.databaseName = defaultDatabaseName
	a.dbSpecified = false
	a.transactionMu = &sync.Mutex{}

	if len(params) == 1 {
		switch p1 := params[0].(type) {
		case bool:
			a.dbSpecified = p1
		case string:
			a.databaseName = p1
		default:
			return nil, errors.New("wrong format")
		}
	} else if len(params) == 2 {
		switch p2 := params[1].(type) {
		case bool:
			a.dbSpecified = p2
			p1, ok := params[0].(string)
			if !ok {
				return nil, errors.New("wrong format")
			}
			a.databaseName = p1
		case string:
			p1, ok := params[0].(string)
			if !ok {
				return nil, errors.New("wrong format")
			}
			a.databaseName = p1
			a.tableName = p2
		default:
			return nil, errors.New("wrong format")
		}
	} else if len(params) == 3 {
		if p3, ok := params[2].(bool); ok {
			a.dbSpecified = p3
			a.databaseName = params[0].(string)
			a.tableName = params[1].(string)
		} else {
			return nil, errors.New("wrong format")
		}
	} else if len(params) != 0 {
		return nil, errors.New("too many parameters")
	}

	// Open the DB, creating it if it does not exist.
	// 打开数据库，不存在则创建。
	err := a.Open()
	if err != nil {
		return nil, err
	}

	// Call the destructor when the object is released.
	// 对象释放时调用析构函数。
	runtime.SetFinalizer(a, finalizer)

	return a, nil
}

// NewAdapterByDBUseTableName creates a gorm-adapter from an existing Gorm
// instance plus the given table prefix and table name.
// Example: gormadapter.NewAdapterByDBUseTableName(&db, "cms", "casbin")
// generates the table name "cms_casbin" automatically.
// NewAdapterByDBUseTableName 用既有 Gorm 实例与指定的表前缀、表名创建适配器。
// 例：gormadapter.NewAdapterByDBUseTableName(&db, "cms", "casbin")
// 会自动生成 "cms_casbin" 这样的表名。
func NewAdapterByDBUseTableName(db *gorm.DB, prefix string, tableName string) (*Adapter, error) {
	if len(tableName) == 0 {
		tableName = defaultTableName
	}

	a := &Adapter{
		tablePrefix:   prefix,
		tableName:     tableName,
		transactionMu: &sync.Mutex{},
	}

	a.db = db.Scopes(a.casbinRuleTable()).Session(&gorm.Session{Context: db.Statement.Context})

	err := a.createTable()
	if err != nil {
		return nil, err
	}

	return a, nil
}

// InitDbResolver enables multiple-database support.
// InitDbResolver 支持多数据库。示例：
//
//	dbPool, err := InitDbResolver([]gorm.Dialector{mysql.Open(dsn), mysql.Open(dsn2)}, []string{"casbin1", "casbin2"})
//	a := NewAdapterByMulDb(dbPool, "casbin1", "", "casbin_rule1")
//	a = NewAdapterByMulDb(dbPool, "casbin2", "", "casbin_rule2")
func InitDbResolver(dbArr []gorm.Dialector, dbNames []string) (DbPool, error) {
	if len(dbArr) == 0 {
		panic("dbArr len is 0")
	}
	source, e := gorm.Open(dbArr[0])
	if e != nil {
		panic(e.Error())
	}
	var p specificPolicy
	p = 0
	err := source.Use(dbresolver.Register(dbresolver.Config{Policy: &p, Sources: dbArr}))
	dbMap := make(map[string]specificPolicy)
	for i := 0; i < len(dbNames); i++ {
		dbMap[dbNames[i]] = specificPolicy(i)
	}
	return DbPool{dbMap: dbMap, policy: &p, source: source}, err
}

// NewAdapterByMulDb creates an adapter on one database of a DbPool
// | NewAdapterByMulDb 在 DbPool 中的某个数据库上创建适配器.
func NewAdapterByMulDb(dbPool DbPool, dbName string, prefix string, tableName string) (*Adapter, error) {
	// switch to the selected DB
	// 切换到指定的数据库
	db := dbPool.switchDb(dbName)

	return NewAdapterByDBUseTableName(db, prefix, tableName)
}

// NewFilteredAdapter is the constructor for a filtered adapter.
// Casbin will not automatically call LoadPolicy() for a filtered adapter.
// NewFilteredAdapter 是过滤适配器的构造函数。
// 对过滤适配器 Casbin 不会自动调用 LoadPolicy()。
func NewFilteredAdapter(driverName string, dataSourceName string, params ...interface{}) (*Adapter, error) {
	adapter, err := NewAdapter(driverName, dataSourceName, params...)
	if err != nil {
		return nil, err
	}
	adapter.isFiltered = true
	return adapter, err
}

// NewFilteredAdapterByDB is the constructor for a filtered adapter over an
// existing DB. Casbin will not automatically call LoadPolicy() for a filtered
// adapter.
// NewFilteredAdapterByDB 基于既有数据库构造过滤适配器。
// 对过滤适配器 Casbin 不会自动调用 LoadPolicy()。
func NewFilteredAdapterByDB(db *gorm.DB, prefix string, tableName string) (*Adapter, error) {
	adapter := &Adapter{
		tablePrefix:   prefix,
		tableName:     tableName,
		isFiltered:    true,
		transactionMu: &sync.Mutex{},
	}
	adapter.db = db.Scopes(adapter.casbinRuleTable()).Session(&gorm.Session{Context: db.Statement.Context})

	return adapter, nil
}

// NewAdapterByDB creates a gorm-adapter from an existing Gorm instance
// | NewAdapterByDB 用既有 Gorm 实例创建 gorm-adapter.
func NewAdapterByDB(db *gorm.DB) (*Adapter, error) {
	return NewAdapterByDBUseTableName(db, "", defaultTableName)
}

// NewTransactionalAdapter creates an adapter that supports the new transaction
// interface. It is an alias for NewAdapter, for clarity when used with a
// TransactionalEnforcer.
// NewTransactionalAdapter 创建支持新事务接口的适配器。
// 它是 NewAdapter 的别名，方便与 TransactionalEnforcer 搭配时表意清晰。
func NewTransactionalAdapter(driverName string, dataSourceName string, params ...interface{}) (*Adapter, error) {
	return NewAdapter(driverName, dataSourceName, params...)
}

// NewTransactionalAdapterByDB creates an adapter from an existing DB that
// supports the new transaction interface. It is an alias for NewAdapterByDB,
// for clarity when used with a TransactionalEnforcer.
// NewTransactionalAdapterByDB 基于既有数据库创建支持新事务接口的适配器。
// 它是 NewAdapterByDB 的别名，方便与 TransactionalEnforcer 搭配时表意清晰。
func NewTransactionalAdapterByDB(db *gorm.DB) (*Adapter, error) {
	return NewAdapterByDB(db)
}

// TurnOffAutoMigrate disables the constructors' auto-migration of the policy
// table; the application must create the table itself
// | TurnOffAutoMigrate 关闭构造函数对策略表的自动迁移，改由应用自行建表.
func TurnOffAutoMigrate(db *gorm.DB) {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}

	ctx = context.WithValue(ctx, disableMigrateKey, false)

	*db = *db.WithContext(ctx)
}

// NewAdapterByDBWithCustomTable migrates the caller-supplied table struct
// instead of CasbinRule | NewAdapterByDBWithCustomTable 迁移调用方自定义的表结构而不是 CasbinRule.
func NewAdapterByDBWithCustomTable(db *gorm.DB, t interface{}, tableName ...string) (*Adapter, error) {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}

	ctx = context.WithValue(ctx, customTableKey, t)

	curTableName := defaultTableName
	if len(tableName) > 0 {
		curTableName = tableName[0]
	}

	return NewAdapterByDBUseTableName(db.WithContext(ctx), "", curTableName)
}

func openDBConnection(driverName, dataSourceName string) (*gorm.DB, error) {
	var err error
	var db *gorm.DB
	config := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}
	if driverName == "postgres" {
		db, err = gorm.Open(postgres.Open(dataSourceName), config)
	} else if driverName == "mysql" {
		db, err = gorm.Open(mysql.Open(dataSourceName), config)
	} else if driverName == "sqlserver" {
		db, err = gorm.Open(sqlserver.Open(dataSourceName), config)
	} else if driverName == "sqlite3" {
		db, err = gorm.Open(sqlite.Open(dataSourceName), config)
	} else {
		return nil, errors.New("Database dialect '" + driverName + "' is not supported. Supported databases are postgres, mysql, sqlserver and sqlite3")
	}
	if err != nil {
		return nil, err
	}
	return db, err
}

func (a *Adapter) createDatabase() error {
	var err error
	db, err := openDBConnection(a.driverName, a.dataSourceName)
	if err != nil {
		return err
	}
	if a.driverName == "postgres" {
		if err = db.Exec("CREATE DATABASE " + a.databaseName).Error; err != nil {
			// 42P04 is duplicate_database.
			// 42P04 是 duplicate_database（库已存在）。
			if strings.Contains(fmt.Sprintf("%s", err), "42P04") {
				return nil
			}
		}
	} else if a.driverName != "sqlite3" && a.driverName != "sqlserver" {
		err = db.Exec("CREATE DATABASE IF NOT EXISTS " + a.databaseName).Error
	}
	if err != nil {
		return err
	}
	return nil
}

// Open connects to the database, creating it when needed, and prepares the
// table | Open 连接数据库（必要时创建）并准备策略表.
func (a *Adapter) Open() error {
	var err error
	var db *gorm.DB

	if a.dbSpecified {
		db, err = openDBConnection(a.driverName, a.dataSourceName)
		if err != nil {
			return err
		}
	} else {
		if err = a.createDatabase(); err != nil {
			return err
		}
		if a.driverName == "postgres" {
			db, err = openDBConnection(a.driverName, a.dataSourceName+" dbname="+a.databaseName)
		} else if a.driverName == "sqlite3" {
			db, err = openDBConnection(a.driverName, a.dataSourceName)
		} else if a.driverName == "sqlserver" {
			db, err = openDBConnection(a.driverName, a.dataSourceName+"?database="+a.databaseName)
		} else {
			db, err = openDBConnection(a.driverName, a.dataSourceName+a.databaseName)
		}
		if err != nil {
			return err
		}
	}

	a.db = db.Scopes(a.casbinRuleTable()).Session(&gorm.Session{})
	return a.createTable()
}

// AddLogger adds a logger to the db session | AddLogger 给 db 会话加上 logger.
func (a *Adapter) AddLogger(l logger.Interface) {
	a.db = a.db.Session(&gorm.Session{Logger: l, Context: a.db.Statement.Context})
}

// Close closes the underlying connection | Close 关闭底层连接.
func (a *Adapter) Close() error {
	finalizer(a)
	return nil
}

// getTableInstance returns a new row instance for the table
// | getTableInstance 返回该表的新行实例.
func (a *Adapter) getTableInstance() *CasbinRule {
	return &CasbinRule{}
}

func (a *Adapter) getFullTableName() string {
	if a.tablePrefix != "" {
		if strings.HasSuffix(a.tablePrefix, "_") {
			return a.tablePrefix + a.tableName
		}
		return a.tablePrefix + "_" + a.tableName
	}
	return a.tableName
}

func (a *Adapter) casbinRuleTable() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		tableName := a.getFullTableName()
		return db.Table(tableName)
	}
}

func (a *Adapter) createTable() error {
	disableMigrate := a.db.Statement.Context.Value(disableMigrateKey)
	if disableMigrate != nil {
		return nil
	}

	t := a.db.Statement.Context.Value(customTableKey)

	if t != nil {
		return a.db.AutoMigrate(t)
	}

	t = a.getTableInstance()
	if err := a.db.AutoMigrate(t); err != nil {
		return err
	}

	tableName := a.getFullTableName()
	index := strings.ReplaceAll("idx_"+tableName, ".", "_")
	hasIndex := a.db.Migrator().HasIndex(t, index)
	if !hasIndex {
		if err := a.db.Exec(fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (ptype,v0,v1,v2,v3,v4,v5)", index, tableName)).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) dropTable() error {
	t := a.db.Statement.Context.Value(customTableKey)
	if t == nil {
		return a.db.Migrator().DropTable(a.getTableInstance())
	}

	return a.db.Migrator().DropTable(t)
}

func policyValues(line CasbinRule) ([]string, error) {
	if line.Ptype == "" {
		return nil, errors.New("gorm adapter: empty policy type")
	}
	values := []string{line.Ptype, line.V0, line.V1, line.V2, line.V3, line.V4, line.V5}
	for len(values) > 1 && values[len(values)-1] == "" {
		values = values[:len(values)-1]
	}
	return values, nil
}

func loadPolicyLine(line CasbinRule, model model.Model) error {
	values, err := policyValues(line)
	if err != nil {
		return err
	}
	return persist.LoadPolicyArray(values, model)
}

// LoadPolicy loads policy from the database | LoadPolicy 从数据库加载策略.
func (a *Adapter) LoadPolicy(model model.Model) error {
	return a.LoadPolicyCtx(context.Background(), model)
}

// LoadPolicyCtx loads policy from the database under the given context
// | LoadPolicyCtx 在给定 context 下从数据库加载策略.
func (a *Adapter) LoadPolicyCtx(ctx context.Context, model model.Model) error {
	var lines []CasbinRule
	if err := a.db.WithContext(ctx).Order("ID").Find(&lines).Error; err != nil {
		return err
	}
	err := a.Preview(&lines, model)
	if err != nil {
		return err
	}
	for _, line := range lines {
		err := loadPolicyLine(line, model)
		if err != nil {
			return err
		}
	}

	return nil
}

// LoadFilteredPolicy loads only the policy rules matching the filter
// | LoadFilteredPolicy 只加载匹配过滤条件的策略规则.
func (a *Adapter) LoadFilteredPolicy(model model.Model, filter interface{}) error {
	var lines []CasbinRule

	batchFilter := BatchFilter{
		filters: []Filter{},
	}
	switch filterValue := filter.(type) {
	case Filter:
		batchFilter.filters = []Filter{filterValue}
	case *Filter:
		batchFilter.filters = []Filter{*filterValue}
	case []Filter:
		batchFilter.filters = filterValue
	case BatchFilter:
		batchFilter = filterValue
	case *BatchFilter:
		batchFilter = *filterValue
	default:
		return errors.New("unsupported filter type")
	}

	for _, f := range batchFilter.filters {
		if err := a.db.Scopes(a.filterQuery(a.db, f)).Order("ID").Find(&lines).Error; err != nil {
			return err
		}

		for _, line := range lines {
			err := loadPolicyLine(line, model)
			if err != nil {
				return err
			}
		}
	}
	a.isFiltered = true

	return nil
}

// IsFiltered returns true if the loaded policy has been filtered
// | IsFiltered 报告已加载的策略是否经过过滤.
func (a *Adapter) IsFiltered() bool {
	return a.isFiltered
}

// filterQuery builds the gorm query matching the rule filter, for use within a
// scope | filterQuery 构造与过滤条件匹配的 gorm 查询，供 scope 使用.
func (a *Adapter) filterQuery(db *gorm.DB, filter Filter) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(filter.Ptype) > 0 {
			db = db.Where("ptype in (?)", filter.Ptype)
		}
		if len(filter.V0) > 0 {
			db = db.Where("v0 in (?)", filter.V0)
		}
		if len(filter.V1) > 0 {
			db = db.Where("v1 in (?)", filter.V1)
		}
		if len(filter.V2) > 0 {
			db = db.Where("v2 in (?)", filter.V2)
		}
		if len(filter.V3) > 0 {
			db = db.Where("v3 in (?)", filter.V3)
		}
		if len(filter.V4) > 0 {
			db = db.Where("v4 in (?)", filter.V4)
		}
		if len(filter.V5) > 0 {
			db = db.Where("v5 in (?)", filter.V5)
		}
		return db
	}
}

func (a *Adapter) savePolicyLine(ptype string, rule []string) CasbinRule {
	line := a.getTableInstance()

	line.Ptype = ptype
	if len(rule) > 0 {
		line.V0 = rule[0]
	}
	if len(rule) > 1 {
		line.V1 = rule[1]
	}
	if len(rule) > 2 {
		line.V2 = rule[2]
	}
	if len(rule) > 3 {
		line.V3 = rule[3]
	}
	if len(rule) > 4 {
		line.V4 = rule[4]
	}
	if len(rule) > 5 {
		line.V5 = rule[5]
	}

	return *line
}

// SavePolicy saves the policy to the database | SavePolicy 将策略保存到数据库.
func (a *Adapter) SavePolicy(model model.Model) error {
	return a.SavePolicyCtx(context.Background(), model)
}

// SavePolicyCtx replaces policies in one transaction. DELETE is used instead
// of TRUNCATE because MySQL TRUNCATE implicitly commits. The ID sequence is
// not reset; readers and failed writers never observe an intermediate empty
// table.
// SavePolicyCtx 在单个事务里整体替换策略。用 DELETE 而不是 TRUNCATE，
// 因为 MySQL 的 TRUNCATE 会隐式提交。ID 序列不重置；读方与失败的写方
// 都不会看到中间态的空表。
func (a *Adapter) SavePolicyCtx(ctx context.Context, model model.Model) error {
	return a.db.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(a.getTableInstance()).Error; err != nil {
			return err
		}
		const batchSize = 1000
		lines := make([]CasbinRule, 0, batchSize)
		flush := func() error {
			if len(lines) == 0 {
				return nil
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lines).Error; err != nil {
				return err
			}
			lines = lines[:0]
			return nil
		}
		for _, section := range []string{"p", "g"} {
			for ptype, ast := range model[section] {
				for _, rule := range ast.Policy {
					lines = append(lines, a.savePolicyLine(ptype, rule))
					if len(lines) == batchSize {
						if err := flush(); err != nil {
							return err
						}
					}
				}
			}
		}
		return flush()
	})
}

// AddPolicy adds a policy rule to the storage | AddPolicy 向存储添加一条策略规则.
func (a *Adapter) AddPolicy(sec string, ptype string, rule []string) error {
	return a.AddPolicyCtx(context.Background(), sec, ptype, rule)
}

// AddPolicyCtx adds a policy rule to the storage under the given context
// | AddPolicyCtx 在给定 context 下向存储添加一条策略规则.
func (a *Adapter) AddPolicyCtx(ctx context.Context, sec string, ptype string, rule []string) error {
	line := a.savePolicyLine(ptype, rule)
	err := a.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error
	return err
}

// RemovePolicy removes a policy rule from the storage | RemovePolicy 从存储删除一条策略规则.
func (a *Adapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return a.RemovePolicyCtx(context.Background(), sec, ptype, rule)
}

// RemovePolicyCtx removes a policy rule from the storage under the given
// context | RemovePolicyCtx 在给定 context 下从存储删除一条策略规则.
func (a *Adapter) RemovePolicyCtx(ctx context.Context, sec string, ptype string, rule []string) error {
	line := a.savePolicyLine(ptype, rule)
	// can't use db.Delete as we're not using the primary key
	// (see https://gorm.io/docs/update.html)
	// 不能用 db.Delete，因为不是按主键删除（见 https://gorm.io/docs/update.html）。
	err := a.exactDelete(ctx, a.db, line)
	return err
}

// AddPolicies adds multiple policy rules to the storage
// | AddPolicies 向存储添加多条策略规则.
func (a *Adapter) AddPolicies(sec string, ptype string, rules [][]string) error {
	var lines []CasbinRule
	for _, rule := range rules {
		line := a.savePolicyLine(ptype, rule)
		lines = append(lines, line)
	}
	return a.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&lines).Error
}

// AddPoliciesCtx adds multiple policy rules to the storage under the given
// context | AddPoliciesCtx 在给定 context 下向存储添加多条策略规则.
func (a *Adapter) AddPoliciesCtx(ctx context.Context, sec string, ptype string, rules [][]string) error {
	var lines []CasbinRule
	for _, rule := range rules {
		line := a.savePolicyLine(ptype, rule)
		lines = append(lines, line)
	}
	return a.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&lines).Error
}

// Transaction performs a set of operations within a transaction
// | Transaction 在一个事务内执行一组操作.
func (a *Adapter) Transaction(e casbin.IEnforcer, fc func(casbin.IEnforcer) error, opts ...*sql.TxOptions) error {
	// ensure the transactionMu is initialized
	// 确保 transactionMu 已初始化
	if a.transactionMu == nil {
		a.muInitialize.Do(func() {
			if a.transactionMu == nil {
				a.transactionMu = &sync.Mutex{}
			}
		})
	}

	// check adapter type
	// 检查适配器类型
	adapter, ok := e.GetAdapter().(*Adapter)
	if !ok {
		return errors.New("expected adapter of type Adapter, but got incompatible type")
	}

	// check whether we are already in a transaction, by checking whether the
	// current adapter's connection pool is a *sql.Tx
	// 通过当前适配器的连接池是否为 *sql.Tx 判断是否已处于事务中
	if _, isTxAdapter := adapter.db.Statement.ConnPool.(*sql.Tx); isTxAdapter {
		// already in a transaction: create a savepoint for the nested one
		// 已在事务中：为嵌套事务创建保存点
		savepointName := fmt.Sprintf("casbin_nested_%d", time.Now().UnixNano())

		// create savepoint
		// 创建保存点
		if err := adapter.db.SavePoint(savepointName).Error; err != nil {
			return errors.Wrap(err, "failed to create savepoint for nested transaction")
		}

		// save model state before the inner transaction
		// 保存内层事务执行前的模型状态
		originalModel := e.GetModel().Copy()

		err := fc(e)
		if err != nil {
			// roll back database changes
			// 回滚数据库变更
			if rollbackErr := adapter.db.RollbackTo(savepointName).Error; rollbackErr != nil {
				return errors.Wrap(rollbackErr, "failed to rollback savepoint")
			}
			// restore model state to undo the inner transaction's changes
			// 恢复模型状态，撤销内层事务的改动
			e.SetModel(originalModel)
			fmt.Printf("Warning: Inner transaction failed and was rolled back: %v\n", err)
			// don't return the error, so the outer transaction can continue
			// 不返回错误，让外层事务继续
			return nil
		}

		return nil
	}

	// lock transactionMu to make the transaction thread-safe
	// 加锁 transactionMu，保证事务线程安全
	a.transactionMu.Lock()
	defer a.transactionMu.Unlock()

	// save the original adapter
	// 保存原适配器
	originalAdapter := adapter.Copy()

	// use GORM's transaction support
	// 使用 GORM 的事务能力
	err := adapter.db.Transaction(func(tx *gorm.DB) error {
		// create a transaction adapter
		// 创建事务适配器
		txAdapter, err := NewAdapterByDB(tx)
		if err != nil {
			return errors.Wrap(err, "failed to initialize gorm adapter")
		}

		// temporarily set the transaction adapter
		// 临时替换为事务适配器
		e.SetAdapter(txAdapter)

		// execute the transaction function
		// 执行事务函数
		err = fc(e)
		if err != nil {
			return errors.Wrap(err, "failed transactional policy operations")
		}

		return nil
	}, opts...)

	// restore the original adapter
	// 恢复原适配器
	e.SetAdapter(originalAdapter)

	if err != nil {
		// LoadPolicy is called only when the transaction fails. It is
		// expensive, but failures are rare thanks to earlier validation.
		// After a failure the in-memory model may be out of sync, so
		// LoadPolicy restores consistency by reloading from the database.
		// 只有事务失败时才会调用 LoadPolicy。该操作开销大，
		// 但前面各层已有校验，失败很少见。事务失败后内存模型可能失同步，
		// 需要靠 LoadPolicy 从数据库重载来恢复一致性。
		if loadErr := e.LoadPolicy(); loadErr != nil {
			return errors.Wrap(loadErr, "failed to load policy after transaction failure")
		}
		return errors.Wrap(err, "transaction execution failed")
	}

	return nil
}

// BeginTransaction implements the TransactionalAdapter interface. It starts a
// new database transaction and returns a TransactionContext.
// BeginTransaction 实现 TransactionalAdapter 接口：
// 开启一个数据库事务并返回 TransactionContext。
func (a *Adapter) BeginTransaction(ctx context.Context) (persist.TransactionContext, error) {
	// start the GORM database transaction
	// 开启 GORM 数据库事务
	tx := a.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}

	return &GormTransactionContext{
		tx:        tx,
		ctx:       ctx,
		adapter:   a,
		tableName: a.tableName,
	}, nil
}

// GormTransactionContext implements the persist.TransactionContext interface.
// It provides transaction control methods and returns a transaction-aware
// adapter.
// GormTransactionContext 实现 persist.TransactionContext 接口，
// 提供事务控制方法，并返回事务内可用的适配器。
type GormTransactionContext struct {
	tx         *gorm.DB
	ctx        context.Context
	adapter    *Adapter
	tableName  string
	committed  bool
	rolledBack bool
}

// Commit commits the database transaction | Commit 提交数据库事务.
func (gtx *GormTransactionContext) Commit() error {
	if gtx.committed || gtx.rolledBack {
		return errors.New("transaction already finished")
	}

	err := gtx.tx.Commit().Error
	if err == nil {
		gtx.committed = true
	}
	return err
}

// Rollback rolls back the database transaction | Rollback 回滚数据库事务.
func (gtx *GormTransactionContext) Rollback() error {
	if gtx.committed || gtx.rolledBack {
		return errors.New("transaction already finished")
	}

	err := gtx.tx.Rollback().Error
	if err == nil {
		gtx.rolledBack = true
	}
	return err
}

// GetAdapter returns an adapter that operates within this transaction.
// All policy operations through this adapter are part of the transaction.
// GetAdapter 返回在本事务内操作的适配器，
// 经由该适配器做的所有策略操作都属于这个事务。
func (gtx *GormTransactionContext) GetAdapter() persist.Adapter {
	return &Adapter{
		driverName:     gtx.adapter.driverName,
		dataSourceName: gtx.adapter.dataSourceName,
		databaseName:   gtx.adapter.databaseName,
		tablePrefix:    gtx.adapter.tablePrefix,
		tableName:      gtx.tableName,
		dbSpecified:    gtx.adapter.dbSpecified,
		// use the transaction connection | 使用事务连接
		db:         gtx.tx,
		isFiltered: gtx.adapter.isFiltered,
		// no transactionMu needed: each transaction has its own adapter
		// 不需要 transactionMu：每个事务有自己的适配器
	}
}

// RemovePolicies removes multiple policy rules from the storage
// | RemovePolicies 从存储删除多条策略规则.
func (a *Adapter) RemovePolicies(sec string, ptype string, rules [][]string) error {
	return a.RemovePoliciesCtx(context.Background(), sec, ptype, rules)
}

// RemovePoliciesCtx removes multiple policy rules from the storage in one
// transaction | RemovePoliciesCtx 在单个事务内从存储删除多条策略规则.
func (a *Adapter) RemovePoliciesCtx(ctx context.Context, sec string, ptype string, rules [][]string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		for _, rule := range rules {
			line := a.savePolicyLine(ptype, rule)
			// can't use db.Delete as we're not using the primary key
			// (see https://gorm.io/docs/update.html)
			// 不能用 db.Delete，因为不是按主键删除（见 https://gorm.io/docs/update.html）。
			if err := a.exactDelete(ctx, tx, line); err != nil {
				// returning the error rolls back the whole transaction;
				// swallowing it would let the caller believe the rules were deleted
				// 返回错误才会回滚整个事务；吞掉它会让调用方以为规则已删除
				return err
			}
		}
		return nil
	})
}

// RemoveFilteredPolicy removes the policy rules matching the filter from the
// storage | RemoveFilteredPolicy 从存储删除匹配过滤条件的策略规则.
func (a *Adapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	return a.RemoveFilteredPolicyCtx(context.Background(), sec, ptype, fieldIndex, fieldValues...)
}

// RemoveFilteredPolicyCtx removes the policy rules matching the filter from
// the storage under the given context
// | RemoveFilteredPolicyCtx 在给定 context 下从存储删除匹配过滤条件的策略规则.
func (a *Adapter) RemoveFilteredPolicyCtx(ctx context.Context, sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	line := a.getTableInstance()

	line.Ptype = ptype

	if fieldIndex == -1 {
		return a.rawDelete(ctx, a.db, *line)
	}
	// The valid range is [0, 5]; out of range no field gets assigned and
	// rawDelete degenerates to deleting by ptype alone, wiping out the whole
	// policy type. Negative values other than -1 behave the same.
	// 合法范围是 [0, 5]；越界时一个字段都不会被赋值，rawDelete 退化成只按 ptype 删，
	// 整类规则都没了。-1 以外的负数同理。
	if fieldIndex < 0 || fieldIndex > 5 {
		return fmt.Errorf("fieldIndex %d out of range [0, 5]", fieldIndex)
	}

	err := checkQueryField(fieldValues)
	if err != nil {
		return err
	}

	if fieldIndex <= 0 && 0 < fieldIndex+len(fieldValues) {
		line.V0 = fieldValues[0-fieldIndex]
	}
	if fieldIndex <= 1 && 1 < fieldIndex+len(fieldValues) {
		line.V1 = fieldValues[1-fieldIndex]
	}
	if fieldIndex <= 2 && 2 < fieldIndex+len(fieldValues) {
		line.V2 = fieldValues[2-fieldIndex]
	}
	if fieldIndex <= 3 && 3 < fieldIndex+len(fieldValues) {
		line.V3 = fieldValues[3-fieldIndex]
	}
	if fieldIndex <= 4 && 4 < fieldIndex+len(fieldValues) {
		line.V4 = fieldValues[4-fieldIndex]
	}
	if fieldIndex <= 5 && 5 < fieldIndex+len(fieldValues) {
		line.V5 = fieldValues[5-fieldIndex]
	}
	err = a.rawDelete(ctx, a.db, *line)
	return err
}

// checkQueryField makes sure the fields are not all empty strings ("" )
// | checkQueryField 确保查询字段不会全为空字符串（""）.
func checkQueryField(fieldValues []string) error {
	for _, fieldValue := range fieldValues {
		if fieldValue != "" {
			return nil
		}
	}
	return errors.New("the query field cannot all be empty string (\"\"), please check")
}

// exactDelete deletes exactly one rule: v0..v5 all take part in the match and
// an empty string only matches an empty value. rawDelete treats empty fields
// as "unrestricted", which is the filter semantics of RemoveFilteredPolicy;
// used for an exact delete, removing (bob,"",GET) would also remove
// (bob,/y,GET).
// exactDelete 精确删除一条规则：v0..v5 全部参与匹配，空字符串只匹配空值。
// rawDelete 把空字段当成「不限制」，那是 RemoveFilteredPolicy 的过滤语义；
// 用在精确删除上，删 (bob,"",GET) 会连 (bob,/y,GET) 一起删掉。
func (a *Adapter) exactDelete(ctx context.Context, db *gorm.DB, line CasbinRule) error {
	return db.WithContext(ctx).Delete(a.getTableInstance(),
		"ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ? AND v4 = ? AND v5 = ?",
		line.Ptype, line.V0, line.V1, line.V2, line.V3, line.V4, line.V5).Error
}

func (a *Adapter) rawDelete(ctx context.Context, db *gorm.DB, line CasbinRule) error {
	queryArgs := []interface{}{line.Ptype}

	queryStr := "ptype = ?"
	if line.V0 != "" {
		queryStr += " and v0 = ?"
		queryArgs = append(queryArgs, line.V0)
	}
	if line.V1 != "" {
		queryStr += " and v1 = ?"
		queryArgs = append(queryArgs, line.V1)
	}
	if line.V2 != "" {
		queryStr += " and v2 = ?"
		queryArgs = append(queryArgs, line.V2)
	}
	if line.V3 != "" {
		queryStr += " and v3 = ?"
		queryArgs = append(queryArgs, line.V3)
	}
	if line.V4 != "" {
		queryStr += " and v4 = ?"
		queryArgs = append(queryArgs, line.V4)
	}
	if line.V5 != "" {
		queryStr += " and v5 = ?"
		queryArgs = append(queryArgs, line.V5)
	}
	args := append([]interface{}{queryStr}, queryArgs...)
	err := db.WithContext(ctx).Delete(a.getTableInstance(), args...).Error
	return err
}

func appendWhere(line CasbinRule) (string, []interface{}) {
	queryArgs := []interface{}{line.Ptype}

	queryStr := "ptype = ?"
	if line.V0 != "" {
		queryStr += " and v0 = ?"
		queryArgs = append(queryArgs, line.V0)
	}
	if line.V1 != "" {
		queryStr += " and v1 = ?"
		queryArgs = append(queryArgs, line.V1)
	}
	if line.V2 != "" {
		queryStr += " and v2 = ?"
		queryArgs = append(queryArgs, line.V2)
	}
	if line.V3 != "" {
		queryStr += " and v3 = ?"
		queryArgs = append(queryArgs, line.V3)
	}
	if line.V4 != "" {
		queryStr += " and v4 = ?"
		queryArgs = append(queryArgs, line.V4)
	}
	if line.V5 != "" {
		queryStr += " and v5 = ?"
		queryArgs = append(queryArgs, line.V5)
	}
	return queryStr, queryArgs
}

// UpdatePolicy updates a policy rule in the DB | UpdatePolicy 更新数据库中的一条策略规则.
func (a *Adapter) UpdatePolicy(sec string, ptype string, oldRule, newPolicy []string) error {
	oldLine := a.savePolicyLine(ptype, oldRule)
	newLine := a.savePolicyLine(ptype, newPolicy)
	return a.db.Model(&oldLine).Where(&oldLine).Updates(newLine).Error
}

// UpdatePolicies updates multiple policy rules in one transaction
// | UpdatePolicies 在单个事务内更新多条策略规则.
func (a *Adapter) UpdatePolicies(sec string, ptype string, oldRules, newRules [][]string) error {
	oldPolicies := make([]CasbinRule, 0, len(oldRules))
	newPolicies := make([]CasbinRule, 0, len(oldRules))
	for _, oldRule := range oldRules {
		oldPolicies = append(oldPolicies, a.savePolicyLine(ptype, oldRule))
	}
	for _, newRule := range newRules {
		newPolicies = append(newPolicies, a.savePolicyLine(ptype, newRule))
	}
	tx := a.db.Begin()
	for i := range oldPolicies {
		if err := tx.Model(&oldPolicies[i]).Where(&oldPolicies[i]).Updates(newPolicies[i]).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

// UpdateFilteredPolicies replaces the rules matching the filter with
// newPolicies and returns the removed old rules
// | UpdateFilteredPolicies 用 newPolicies 替换匹配过滤条件的规则，并返回被删的旧规则.
func (a *Adapter) UpdateFilteredPolicies(sec string, ptype string, newPolicies [][]string, fieldIndex int, fieldValues ...string) ([][]string, error) {
	// UpdateFilteredPolicies deletes the old rules and adds the new ones.
	// UpdateFilteredPolicies 先删旧规则再加新规则。
	line := a.getTableInstance()

	line.Ptype = ptype
	if fieldIndex <= 0 && 0 < fieldIndex+len(fieldValues) {
		line.V0 = fieldValues[0-fieldIndex]
	}
	if fieldIndex <= 1 && 1 < fieldIndex+len(fieldValues) {
		line.V1 = fieldValues[1-fieldIndex]
	}
	if fieldIndex <= 2 && 2 < fieldIndex+len(fieldValues) {
		line.V2 = fieldValues[2-fieldIndex]
	}
	if fieldIndex <= 3 && 3 < fieldIndex+len(fieldValues) {
		line.V3 = fieldValues[3-fieldIndex]
	}
	if fieldIndex <= 4 && 4 < fieldIndex+len(fieldValues) {
		line.V4 = fieldValues[4-fieldIndex]
	}
	if fieldIndex <= 5 && 5 < fieldIndex+len(fieldValues) {
		line.V5 = fieldValues[5-fieldIndex]
	}

	newP := make([]CasbinRule, 0, len(newPolicies))
	oldP := make([]CasbinRule, 0)
	for _, newRule := range newPolicies {
		newP = append(newP, a.savePolicyLine(ptype, newRule))
	}

	tx := a.db.Begin()
	str, args := line.queryString()
	if err := tx.Where(str, args...).Find(&oldP).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Where(str, args...).Delete([]CasbinRule{}).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	for i := range newP {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&newP[i]).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// return the deleted rules
	// 返回被删除的规则
	oldPolicies := make([][]string, 0)
	for _, v := range oldP {
		oldPolicy := v.toStringPolicy()
		oldPolicies = append(oldPolicies, oldPolicy)
	}
	return oldPolicies, tx.Commit().Error
}

// Copy clones the adapter sharing the same db and transaction mutex
// | Copy 克隆适配器，共享同一个 db 与事务锁.
func (a *Adapter) Copy() *Adapter {
	oriAdapter := a.db
	return &Adapter{
		db:             oriAdapter,
		transactionMu:  a.transactionMu,
		driverName:     a.driverName,
		dataSourceName: a.dataSourceName,
		databaseName:   a.databaseName,
		tablePrefix:    a.tablePrefix,
		tableName:      a.tableName,
		dbSpecified:    a.dbSpecified,
		isFiltered:     a.isFiltered,
	}
}

// Preview pre-filters rules to avoid a partially loaded model deep inside the
// load | Preview 预检规则，避免加载深处出现一半成功一半失败.
func (a *Adapter) Preview(rules *[]CasbinRule, model model.Model) error {
	j := 0
	for i, rule := range *rules {
		p, err := policyValues(rule)
		if err != nil {
			return err
		}
		key := p[0]
		sec := key[:1]
		ok, err := model.HasPolicyEx(sec, key, p[1:])
		if err != nil {
			return err
		}
		if ok {
			(*rules)[j], (*rules)[i] = rule, (*rules)[j]
			j++
		}
	}
	(*rules) = (*rules)[j:]
	return nil
}

// GetDb exposes the underlying *gorm.DB | GetDb 返回底层的 *gorm.DB.
func (a *Adapter) GetDb() *gorm.DB {
	return a.db
}

func (c *CasbinRule) queryString() (interface{}, []interface{}) {
	queryArgs := []interface{}{c.Ptype}

	queryStr := "ptype = ?"
	if c.V0 != "" {
		queryStr += " and v0 = ?"
		queryArgs = append(queryArgs, c.V0)
	}
	if c.V1 != "" {
		queryStr += " and v1 = ?"
		queryArgs = append(queryArgs, c.V1)
	}
	if c.V2 != "" {
		queryStr += " and v2 = ?"
		queryArgs = append(queryArgs, c.V2)
	}
	if c.V3 != "" {
		queryStr += " and v3 = ?"
		queryArgs = append(queryArgs, c.V3)
	}
	if c.V4 != "" {
		queryStr += " and v4 = ?"
		queryArgs = append(queryArgs, c.V4)
	}
	if c.V5 != "" {
		queryStr += " and v5 = ?"
		queryArgs = append(queryArgs, c.V5)
	}

	return queryStr, queryArgs
}

func (c *CasbinRule) toStringPolicy() []string {
	policy := make([]string, 0)
	if c.Ptype != "" {
		policy = append(policy, c.Ptype)
	}
	if c.V0 != "" {
		policy = append(policy, c.V0)
	}
	if c.V1 != "" {
		policy = append(policy, c.V1)
	}
	if c.V2 != "" {
		policy = append(policy, c.V2)
	}
	if c.V3 != "" {
		policy = append(policy, c.V3)
	}
	if c.V4 != "" {
		policy = append(policy, c.V4)
	}
	if c.V5 != "" {
		policy = append(policy, c.V5)
	}
	return policy
}

// CombineType represents strategies for combining conditions
// | CombineType 表示条件的组合策略.
type CombineType uint32

const (
	// CombineTypeOr combines conditions with the OR operator | 以 OR 组合条件.
	CombineTypeOr CombineType = iota
	// CombineTypeAnd combines conditions with the AND operator | 以 AND 组合条件.
	CombineTypeAnd
)

// ConditionsToGormQuery converts multiple query conditions into a GORM query
// statement. Conditions can come from Casbin's GetAllowedObjectConditions()
// API; combineType picks how they are combined.
// ConditionsToGormQuery 把多个查询条件转换成 GORM 查询语句。
// 条件可来自 Casbin 的 GetAllowedObjectConditions() API，
// 用 combineType 选择条件的组合方式。
func ConditionsToGormQuery(db *gorm.DB, conditions []string, combineType CombineType) *gorm.DB {
	queryDB := db
	for _, cond := range conditions {
		switch combineType {
		case CombineTypeOr:
			queryDB = queryDB.Or(cond)
		case CombineTypeAnd:
			queryDB = queryDB.Where(cond)
		}
	}
	return queryDB
}
