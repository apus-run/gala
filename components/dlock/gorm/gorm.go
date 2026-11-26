// Package gorm provides a database-row distributed lock working on MySQL and
// PostgreSQL | gorm 提供基于数据库行记录的分布式锁，适用于 MySQL 与 PostgreSQL.
//
// # Notes | 说明
//
// [Init] creates the GORMLock table, which needs a unique index on name, and
// runs before the first lock operation. Lock inserts a row; on a duplicate key
// it inspects the existing row: still unexpired means Lock fails with the
// current owner, expired means take-over guarded by an optimistic version
// check, so at most one contender wins. Unlock deletes the row only when the
// token still matches and the lease has not expired, and Refresh extends
// ExpiredAt only while the token matches and the row has not expired; an old
// holder can never release or revive the next holder's lock. No background
// renewal is started: long-running work must call Refresh before LockTimeout
// elapses. Unlock ignores caller cancellation, so a canceled request or job
// still releases the row.
// [Init] 创建 GORMLock 表（需要 name 唯一索引），在首次加锁前执行。Lock 插入
// 一行；唯一键冲突时检查已有行：未过期则以当前持有者报错，已过期则通过
// version 乐观锁接管，保证最多一个竞争者成功。Unlock 仅在令牌匹配且租约未过期
// 时删除行，Refresh 仅在令牌匹配且行未过期时续写 ExpiredAt；旧持有者无法释放
// 或复活新持有者的锁。不会启动后台续期：长任务必须在 LockTimeout 到期前调用
// Refresh。Unlock 忽略调用方取消，请求/任务取消后仍会释放行。
//
// Expiry timestamps use the application clock, so keep node clocks in sync
// (NTP) or allow a margin for skew.
// 过期时间使用应用时钟，请保持节点时钟同步（NTP）或为时钟漂移留出余量。
//
// # Example | 示例
//
//	if err := gorm.Init(ctx, db); err != nil { // creates/migrates gorm_locks
//		return err
//	}
//	locker, err := gorm.NewLock(ctx, db, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package gorm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/internal/cleanup"
	"github.com/apus-run/gala/components/dlock/internal/opctx"
	"github.com/apus-run/gala/components/dlock/internal/token"
)

// Lock provides a distributed locking mechanism using GORM | Lock 提供基于 GORM 的分布式锁机制。
type Lock struct {
	db               *gorm.DB
	lockName         string
	lockTimeout      time.Duration
	operationTimeout time.Duration
	ownerID          string
	token            string
}

// GORMLock represents a database record for a distributed lock | GORMLock 表示分布式锁的数据库记录。
type GORMLock struct {
	ID      uint   `gorm:"primarykey"`
	Name    string `gorm:"unique"`
	OwnerID string
	// Token holds "<ownerID>@<random>" | Token 保存 "<ownerID>@<随机值>"。
	Token string
	// Version defaults to 0: rows created before this column existed must not
	// stay NULL, or they would never match the optimistic-lock predicate.
	// Version 默认 0：本列出现前创建的旧行不能是 NULL，否则永远匹配不上乐观锁
	// 条件。
	Version int64 `gorm:"default:0"`
	// ExpiredAt is when the lock becomes take-overable | ExpiredAt 是锁可被接管的时刻。
	ExpiredAt time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock initializes a new Lock instance. It performs no I/O; call [Init]
// once before the first Lock/Unlock/Refresh to create or migrate the table.
// NewLock 初始化新的 Lock 实例，构造期不做任何 I/O；首次 Lock/Unlock/Refresh
// 之前调用一次 [Init] 建表或迁移。
func NewLock(_ context.Context, db *gorm.DB, opts ...dlock.Option) (*Lock, error) {
	if db == nil {
		return nil, fmt.Errorf("dlock/gorm: nil gorm db")
	}

	o := dlock.ApplyOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, err
	}

	tok, err := token.New(o.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("dlock/gorm: generate owner token: %w", err)
	}

	return &Lock{
		db:               db,
		lockName:         o.LockName,
		lockTimeout:      o.LockTimeout,
		operationTimeout: o.OperationTimeout,
		ownerID:          o.OwnerID,
		token:            tok,
	}, nil
}

// Init creates or migrates the lock table and repairs legacy rows whose
// version is NULL. Call it once during startup, before any Locker uses the
// table; concurrent calls are not synchronized.
// Init 建表或迁移锁表，并修复 version 为 NULL 的旧行。启动时、任何 Locker 使用
// 该表之前调用一次；并发调用没有同步保护。
func Init(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("dlock/gorm: nil gorm db")
	}
	if err := db.WithContext(ctx).AutoMigrate(&GORMLock{}); err != nil {
		return fmt.Errorf("dlock/gorm: migrate gorm_locks: %w", err)
	}
	if err := db.WithContext(ctx).Model(&GORMLock{}).
		Where("version IS NULL").
		UpdateColumn("version", 0).Error; err != nil {
		return fmt.Errorf("dlock/gorm: backfill version: %w", err)
	}
	// MySQL and PostgreSQL drivers return SQL NULL for the new token column,
	// which cannot be scanned into GORM's string field; repair legacy rows
	// before any read.
	// MySQL/PostgreSQL 驱动会把新 token 列的 SQL NULL 返回给 GORM，无法扫描进
	// string 字段；在任何读取前修复旧行。
	if err := db.WithContext(ctx).Model(&GORMLock{}).
		Where("token IS NULL").
		UpdateColumn("token", "").Error; err != nil {
		return fmt.Errorf("dlock/gorm: backfill token: %w", err)
	}
	return nil
}

// Lock acquires the distributed lock | Lock 获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	now := time.Now()

	for range 2 {
		err := l.db.WithContext(ctx).Create(&GORMLock{
			Name:      l.lockName,
			OwnerID:   l.ownerID,
			Token:     l.token,
			Version:   1,
			ExpiredAt: now.Add(l.lockTimeout),
		}).Error
		if err == nil {
			return nil
		}
		if !isDuplicateEntry(err) {
			return fmt.Errorf("dlock/gorm: acquire lock %q: %w", l.lockName, err)
		}

		done, err := l.takeOver(ctx, now)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		// The row disappeared between Create and takeOver: try to insert again.
		// 行在 Create 与 takeOver 之间被删除：重新尝试插入。
	}

	return fmt.Errorf("%w: lock %q", dlock.ErrLocked, l.lockName)
}

// takeOver inspects the existing row and either reports contention, extends a
// lock this Locker already owns, or takes over an expired one with a version
// CAS. done is false when the row vanished and the caller should retry insert.
// takeOver 检查已有行：报告竞争、延长本 Locker 已持有的锁，或通过 version
// CAS 接管过期锁。行为消失时 done 为 false，调用方应重试插入。
func (l *Lock) takeOver(ctx context.Context, now time.Time) (done bool, err error) {
	db := l.db.WithContext(ctx)

	var current GORMLock
	if err := db.Where("name = ?", l.lockName).First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return true, fmt.Errorf("dlock/gorm: inspect lock %q: %w", l.lockName, err)
	}

	// Retry-friendly: this Locker already owns the row, extend its TTL.
	// 重试友好：本 Locker 已持有该行，延长 TTL 即可。
	if current.Token == l.token {
		res := db.Model(&GORMLock{}).
			Where("name = ? AND token = ?", l.lockName, l.token).
			Updates(map[string]any{
				"expired_at": now.Add(l.lockTimeout),
				"version":    gorm.Expr("version + 1"),
			})
		if res.Error != nil {
			return true, fmt.Errorf("dlock/gorm: extend lock %q: %w", l.lockName, res.Error)
		}
		if res.RowsAffected == 0 {
			// The row vanished between the read and the update, so the lock is
			// free: let the caller retry the insert instead of reporting
			// contention.
			// 行在读与更新之间消失，说明锁已空闲：让调用方重试插入，而不是
			// 报告竞争。
			return false, nil
		}
		return true, nil
	}

	if now.Before(current.ExpiredAt) {
		return true, fmt.Errorf("%w: lock %q held by %q", dlock.ErrLocked, l.lockName, current.OwnerID)
	}

	res := db.Model(&GORMLock{}).
		Where("name = ? AND version = ? AND expired_at <= ?", l.lockName, current.Version, now).
		Updates(map[string]any{
			"owner_id":   l.ownerID,
			"token":      l.token,
			"version":    gorm.Expr("version + 1"),
			"expired_at": now.Add(l.lockTimeout),
		})
	if res.Error != nil {
		return true, fmt.Errorf("dlock/gorm: take over lock %q: %w", l.lockName, res.Error)
	}
	if res.RowsAffected == 0 {
		return true, fmt.Errorf("%w: lock %q", dlock.ErrLocked, l.lockName)
	}
	return true, nil
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	// Release is cleanup: a canceled caller context must not leave the row
	// behind. Keep the values, ignore cancellation, bound the request.
	// 释放属于清理：调用方 ctx 取消不应让行留下；保留值、忽略取消并限时。
	ctx, cancel := cleanup.Context(ctx)
	defer cancel()

	now := time.Now()
	res := l.db.WithContext(ctx).
		// expired_at > now keeps Unlock consistent with Refresh and with the
		// Redis/etcd/Consul backends: an expired lease is not held anymore.
		// expired_at > now 让 Unlock 与 Refresh 以及 Redis/etcd/Consul 后端
		// 一致：已过期的租约不再算持有。
		Where("name = ? AND token = ? AND expired_at > ?", l.lockName, l.token, now).
		Delete(&GORMLock{})
	if res.Error != nil {
		return fmt.Errorf("dlock/gorm: release lock %q: %w", l.lockName, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	return nil
}

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
func (l *Lock) Refresh(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	now := time.Now()

	res := l.db.WithContext(ctx).
		Model(&GORMLock{}).
		Where("name = ? AND token = ? AND expired_at > ?", l.lockName, l.token, now).
		Updates(map[string]any{
			"expired_at": now.Add(l.lockTimeout),
			// Incrementing the version guarantees a changed row, so MySQL's
			// "rows affected" cannot report zero for an unchanged timestamp.
			// 版本递增保证行确有变化，避免 MySQL 因时间戳未变而返回 0 行。
			"version": gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return fmt.Errorf("dlock/gorm: refresh lock %q: %w", l.lockName, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockName)
	}
	return nil
}

// isDuplicateEntry reports whether err is a unique-key violation on MySQL,
// PostgreSQL, or already translated by GORM.
// isDuplicateEntry 判断是否为 MySQL、PostgreSQL 的唯一键冲突，或已被 GORM 翻译。
func isDuplicateEntry(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	if mysqlErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		return mysqlErr.Number == 1062 // MySQL duplicate-entry error code | MySQL 重复条目错误码
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23505" // PostgreSQL unique violation | PostgreSQL 唯一性冲突
	}

	return false
}
