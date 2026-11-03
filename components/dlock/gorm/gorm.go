// Package gorm provides a database-row distributed lock working on MySQL and
// PostgreSQL | gorm 提供基于数据库行记录的分布式锁，适用于 MySQL 与 PostgreSQL.
//
// # Notes | 说明
//
// NewLock runs AutoMigrate on the GORMLock table. Lock inserts a row with a
// unique name; on a duplicate-key conflict it checks the existing row inside
// the same transaction: unexpired means Lock fails with the current owner,
// expired means take-over. A background goroutine renews ExpiredAt every
// LockTimeout/2 until Unlock deletes the row.
// NewLock 会对 GORMLock 表执行 AutoMigrate。Lock 插入以 name 唯一的行；主键
// 冲突时在同一事务内检查已有行：未过期则携当前持有者报错，已过期则接管。
// 后台协程每 LockTimeout/2 续写 ExpiredAt，直到 Unlock 删除该行。
//
// # Example | 示例
//
//	locker, err := gorm.NewLock(ctx, db, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package gorm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
)

// Lock provides a distributed locking mechanism using GORM | Lock 提供基于 GORM 的分布式锁机制。
type Lock struct {
	db          *gorm.DB
	lockName    string
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string
}

// GORMLock represents a database record for a distributed lock | GORMLock 表示分布式锁的数据库记录。
type GORMLock struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"unique"`
	OwnerID   string
	ExpiredAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock initializes a new Lock instance | NewLock 初始化新的 Lock 实例。
func NewLock(_ context.Context, db *gorm.DB, opts ...dlock.Option) (*Lock, error) {
	o := dlock.ApplyOptions(opts...)

	if err := db.AutoMigrate(&GORMLock{}); err != nil {
		return nil, err
	}

	locker := &Lock{
		db:          db,
		ownerID:     o.OwnerID,
		lockName:    o.LockName,
		lockTimeout: o.LockTimeout,
		stopChan:    make(chan struct{}),
	}

	return locker, nil
}

// Lock acquires the distributed lock | Lock 获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	expiredAt := now.Add(l.lockTimeout)

	err := l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&GORMLock{Name: l.lockName, OwnerID: l.ownerID, ExpiredAt: expiredAt}).Error; err != nil {
			if !isDuplicateEntry(err) {
				slog.Error("failed to create lock", "error", err)
				return err
			}

			var lock GORMLock
			if err := tx.First(&lock, "name = ?", l.lockName).Error; err != nil {
				slog.Error("failed to fetch existing lock", "error", err)
				return err
			}

			if !lock.ExpiredAt.Before(now) {
				slog.Warn("lock is already held by another owner", "ownerID", lock.OwnerID)
				return fmt.Errorf("lock is already held by %s", lock.OwnerID)
			}

			lock.OwnerID = l.ownerID
			lock.ExpiredAt = expiredAt
			if err := tx.Save(&lock).Error; err != nil {
				slog.Error("failed to update expired lock", "error", err)
				return err
			}
			slog.Info("Lock expired, updated owner", "lockName", l.lockName, "newOwnerID", l.ownerID)
		}

		l.renewTicker = time.NewTicker(l.lockTimeout / 2)
		go l.refreshLock(ctx)

		slog.Info("Lock acquired", "lockName", l.lockName, "ownerID", l.ownerID)
		return nil
	})

	return err
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.renewTicker != nil {
		l.renewTicker.Stop()
		l.renewTicker = nil
		close(l.stopChan)
		slog.Info("Stopped renewing lock", "lockName", l.lockName)
	}

	err := l.db.Delete(&GORMLock{}, "name = ?", l.lockName).Error
	if err != nil {
		slog.Error("failed to delete lock", "error", err)
		return err
	}

	slog.Info("Lock released", "lockName", l.lockName)
	return nil
}

// Refresh refreshes the lease for the distributed lock | Refresh 为分布式锁的租约续期。
func (l *Lock) Refresh(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	expiredAt := now.Add(l.lockTimeout)

	err := l.db.Model(&GORMLock{}).Where("name = ?", l.lockName).Update("expired_at", expiredAt).Error
	if err != nil {
		slog.Error("failed to renew lock", "error", err)
		return err
	}

	slog.Info("Lock renewed", "lockName", l.lockName, "newExpiration", expiredAt)
	return nil
}

// refreshLock periodically refreshes the lock lease | refreshLock 周期性刷新锁租约。
func (l *Lock) refreshLock(ctx context.Context) {
	for {
		select {
		case <-l.stopChan:
			return
		case <-l.renewTicker.C:
			if err := l.Refresh(ctx); err != nil {
				slog.Error("failed to renew lock", "error", err)
			}
		}
	}
}

// isDuplicateEntry checks if the error is a duplicate entry error for MySQL and PostgreSQL | isDuplicateEntry 判断错误是否为 MySQL 或 PostgreSQL 的重复条目错误。
func isDuplicateEntry(err error) bool {
	if err == nil {
		return false
	}

	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		return mysqlErr.Number == 1062 // MySQL error code for duplicate entry | MySQL 重复条目错误码
	}

	if pgErr, ok := err.(*pgconn.PgError); ok {
		return pgErr.Code == "23505" // PostgreSQL error code for unique violation | PostgreSQL 唯一性冲突错误码
	}

	return false
}
