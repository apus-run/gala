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

// Lock provides a distributed locking mechanism using GORM.
type Lock struct {
	db          *gorm.DB
	lockName    string
	lockTimeout time.Duration
	renewTicker *time.Ticker
	stopChan    chan struct{}
	mu          sync.Mutex
	ownerID     string
}

// GORMLock represents a database record for a distributed lock.
type GORMLock struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"unique"`
	OwnerID   string
	ExpiredAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Ensure Lock implements the Locker interface.
var _ dlock.Locker = (*Lock)(nil)

// NewLock initializes a new Lock instance.
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

// Lock acquires the distributed lock.
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

// Unlock releases the distributed lock.
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

// Refresh refreshes the lease for the distributed lock.
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

// refreshLock periodically refreshes the lock lease.
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

// isDuplicateEntry checks if the error is a duplicate entry error for MySQL and PostgreSQL.
func isDuplicateEntry(err error) bool {
	if err == nil {
		return false
	}

	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		return mysqlErr.Number == 1062 // MySQL error code for duplicate entry
	}

	if pgErr, ok := err.(*pgconn.PgError); ok {
		return pgErr.Code == "23505" // PostgreSQL error code for unique violation
	}

	return false
}
