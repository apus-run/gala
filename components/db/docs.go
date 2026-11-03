// Package db wraps gorm in a context-first Provider for database sessions and transactions | db 包装 gorm，提供以 ctx 贯穿的 Provider 会话与事务抽象.
//
// # Notes | 说明
//
// NewDB accepts any gorm Dialector and always enables TranslateError, so
// driver errors surface as gorm.ErrDuplicatedKey and friends regardless of
// the database in use. DB(ctx) returns a session bound to the caller's
// context; TX stores the transaction in the context and nested TX calls
// degrade to savepoints. A transaction context must not be used across
// goroutines: sql.Tx does not support concurrent queries.
// NewDB 接受任意 gorm Dialector 并强制开启 TranslateError，驱动错误统一翻译为
// gorm.ErrDuplicatedKey 等，与具体数据库无关。DB(ctx) 返回绑定调用方 context
// 的会话；TX 把事务放进 context，嵌套 TX 自动降级为 savepoint。事务 context
// 不能跨 goroutine 并发使用：sql.Tx 不支持并发查询。
//
// Query and repository helpers live in subpackages (where, page, repo, ...);
// this package stays at the session/transaction layer.
// 查询与仓储辅助在子包中（where、page、repo 等），本包只停留在会话与事务这一层。
//
// # Features | 功能
//
// - Provider: DB / TX / Close, ctx propagation for tracing | Provider：DB / TX / Close，ctx 贯穿用于链路追踪
// - Per-call options: WithMaster, WithTransaction, WithDeleted, WithSelectForUpdate, Debug | 每次调用可选：WithMaster、WithTransaction、WithDeleted、WithSelectForUpdate、Debug
// - Unwrap recovers the underlying *gorm.DB | Unwrap 取回底层 *gorm.DB
//
// # Example | 示例
//
//	p, err := db.NewDB(mysql.Open(dsn)) // driver chosen by the caller | 驱动由调用方选择
//	defer p.Close()
//	err = p.TX(ctx, func(ctx context.Context) error {
//		return p.DB(ctx).Create(&User{Name: "a"}).Error
//	})
package db
