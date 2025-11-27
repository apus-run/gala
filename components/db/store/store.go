// Package store provides a generic data store implementation
//
//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks

package store

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db/where"
)

// Provider defines an interface for providing a database connection.
type Provider interface {
	// DB returns the database instance for the given context.
	DB(ctx context.Context, wheres ...where.Where) *gorm.DB

	// TX executes a transactional operation.
	TX(ctx context.Context, fn func(ctx context.Context) error) error
}

// Option defines a function type for configuring the Store.
type Option[T any] func(*Store[T])

// Store represents a generic data store with logging capabilities.
type Store[T any] struct {
	storage Provider
}

// NewStore creates a new instance of Store with the provided DBProvider.
func NewStore[T any](storage Provider) *Store[T] {
	return &Store[T]{
		storage: storage,
	}
}

// db retrieves the database instance and applies the provided where conditions.
func (s *Store[T]) db(ctx context.Context, wheres ...where.Where) *gorm.DB {
	session := s.storage.DB(ctx)
	for _, whr := range wheres {
		if whr != nil {
			session = whr.Where(session)
		}
	}
	return session
}

// Create inserts a new object into the database.
func (s *Store[T]) Create(ctx context.Context, obj *T) error {
	if err := s.db(ctx).Create(obj).Error; err != nil {
		return err
	}
	return nil
}

// Update modifies an existing object in the database.
func (s *Store[T]) Update(ctx context.Context, obj *T) error {
	if err := s.db(ctx).Save(obj).Error; err != nil {
		return err
	}
	return nil
}

// Delete removes an object from the database based on the provided where options.
func (s *Store[T]) Delete(ctx context.Context, opts *where.Options) error {
	err := s.db(ctx, opts).Delete(new(T)).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

// Get retrieves a single object from the database based on the provided where options.
func (s *Store[T]) Get(ctx context.Context, opts *where.Options) (*T, error) {
	var obj T
	if err := s.db(ctx, opts).First(&obj).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("record not found")
		}
		return nil, err
	}
	return &obj, nil
}

// List retrieves a list of objects from the database based on the provided where options.
func (s *Store[T]) List(ctx context.Context, opts *where.Options) (count int64, rets []*T, err error) {
	err = s.db(ctx, opts).Order("id desc").Find(&rets).Offset(-1).Limit(-1).Count(&count).Error
	if err != nil {
		return 0, nil, err
	}
	return count, rets, nil
}

// Count returns the number of objects in the database that match the provided where options.
func (s *Store[T]) Count(ctx context.Context, opts *where.Options) (count int64, err error) {
	err = s.db(ctx, opts).Model(new(T)).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Pluck retrieves a list of values for a specific column from the database based on the provided where options.
func (s *Store[T]) Pluck(ctx context.Context, column string, opts *where.Options) (rets []any, err error) {
	err = s.db(ctx, opts).Model(new(T)).Pluck(column, &rets).Error
	if err != nil {
		return nil, err
	}
	return rets, nil
}
