// Package mongo wraps the official mongo-driver-go with client, database,
// collection, cursor and session handles plus mgo-style helpers
// | 基于官方 mongo-driver-go 提供 client、database、collection、cursor 与
// session 的包装，以及 mgo 风格的工具函数.
//
// # Notes | 说明
//
// Wrappers can record every command they execute (SetLogMode) and run it
// through an interceptor chain (WrapProcessor, InterceptorChain, and the
// zlog-based debug interceptor from NewBuilder). The chain only covers
// commands issued through the wrappers: handles obtained from Client(),
// Collection() or GetDatabase() talk to the driver directly and bypass both
// logging and interceptors.
// 包装器可以记录执行的每条命令（SetLogMode），并让其经过拦截器链
// （WrapProcessor、InterceptorChain，以及 NewBuilder 构建的 zlog 调试
// 拦截器）。该链只覆盖经过包装器发出的命令：通过 Client()、Collection()
// 或 GetDatabase() 拿到的原生句柄直连驱动，不经过日志与拦截器。
//
// Open bundles a Client, Database and Collection from one Config
// (DatabaseName/CollectionName). Sessions and transactions need a replica
// set; the repo subpackage's WithTransaction requires a mongo.SessionContext.
// Open 依据同一份 Config（DatabaseName/CollectionName）组装 Client、
// Database 与 Collection。会话与事务需要副本集；repo 子包的
// WithTransaction 要求传入 mongo.SessionContext。
//
// Constructors do no I/O: NewClient and Open only validate options and the
// driver dials on the first command. Call Ping explicitly to fail fast at
// startup; Close (alias of Disconnect) releases the sockets.
// 构造函数不做 I/O：NewClient 与 Open 只校验选项，驱动在首条命令时才拨号。
// 需要启动即校验连通性请显式调用 Ping；释放连接用 Close（Disconnect 的别名）。
//
// The repo subpackage adds a generic Repository/Query abstraction. Several
// SQL-flavored Query methods are no-ops on MongoDB (With, WithCount, GroupBy,
// Having, Distinct) — see the repo package doc.
// repo 子包提供泛型 Repository/Query 抽象。若干 SQL 风格的 Query 方法在
// MongoDB 上是 no-op（With、WithCount、GroupBy、Having、Distinct），
// 见 repo 包文档。
//
// # Features | 功能
//
// - Debug logging and interceptor chain around wrapped commands | 包装命令的调试日志与拦截器链
// - mgo-style helpers: Set, PrepSort, PrepIndex, Change, Remove, IsDup, NewObjectID | mgo 风格工具：Set、PrepSort、PrepIndex、Change、Remove、IsDup、NewObjectID
// - BufferedWriterMongo batching writes by size and interval | 按大小与间隔批量写入的 BufferedWriterMongo
// - repo: generic repository and fluent query builder | repo：泛型仓库与链式查询构造器
package mongo
