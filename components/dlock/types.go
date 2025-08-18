// Package dlock provides an interface for distributed locking mechanisms.
package dlock

import (
	"context"
	"os"
	"time"
)

// DefaultLockName is the default name used for the distributed lock.
const DefaultLockName = "gala-distributed-lock"

type Client interface {
	// NewLock creates a new Locker instance with the specified key and options.
	NewLock(ctx context.Context, opts ...Option) (Locker, error)
}

// Locker is an interface that defines the methods for a distributed lock.
// It provides methods to acquire, release, and refresh a lock in a distributed system.
type Locker interface {
	// Lock attempts to acquire the lock.
	Lock(ctx context.Context) error

	// Unlock releases the previously acquired lock.
	Unlock(ctx context.Context) error

	// Refresh updates the expiration time of the lock.
	// It should be called periodically to keep the lock active.
	Refresh(ctx context.Context) error
}

// Options holds the configuration for the distributed lock.
type Options struct {
	LockName    string        // Name of the lock
	LockTimeout time.Duration // Duration before the lock expires
	OwnerID     string        // Identifier for the lock owner
}

// Option is a function that modifies Options.
type Option func(o *Options)

// NewOptions initializes Options with default values.
func NewOptions() *Options {
	ownerID, _ := os.Hostname() // Get the hostname as the default owner ID
	return &Options{
		LockName:    DefaultLockName,
		LockTimeout: 10 * time.Second, // Default lock timeout
		OwnerID:     ownerID,          // Set the owner ID
	}
}

// ApplyOptions applies a series of Option functions to configure Options.
func ApplyOptions(opts ...Option) *Options {
	o := NewOptions() // Create a new Options instance with default values
	for _, opt := range opts {
		opt(o) // Apply each option to the Options instance
	}

	return o // Return the configured Options
}

// WithLockName sets the lock name in Options.
func WithLockName(name string) Option {
	return func(o *Options) {
		o.LockName = name // Set the lock name
	}
}

// WithLockTimeout sets the lock timeout in Options.
func WithLockTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.LockTimeout = timeout // Set the lock timeout
	}
}

// WithOwnerID sets the owner ID in Options.
func WithOwnerID(ownerID string) Option {
	return func(o *Options) {
		o.OwnerID = ownerID // Set the owner ID
	}
}
