// Package authz wraps Casbin authorization with configurable model and policy storage.
package authz

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	gormadapter "github.com/casbin/gorm-adapter/v3"

	"gorm.io/gorm"
)

// Authorizer provides synchronized authorization and serialized policy reloads.
// Do not copy an instance after use.
type Authorizer struct {
	engine *casbin.SyncedEnforcer
	mu     sync.Mutex
}

// timedAdapter bounds the database part of each policy load, including the
// constructor's initial load. It does not promise to cancel Casbin's CPU work.
// gorm-adapter's ordinary LoadPolicy uses context.Background, so putting a
// deadline on the GORM session alone would not bound that load.
type timedAdapter struct {
	*gormadapter.Adapter
	timeout time.Duration
}

func (a *timedAdapter) LoadPolicy(m model.Model) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	return a.Adapter.LoadPolicyCtx(ctx, m)
}

// New creates an initialized enforcer using the configured policy adapter.
// Adapters with an already filtered policy are not supported.
// The caller owns injected dependencies and policy synchronization tasks.
func New(opts ...Option) (*Authorizer, error) {
	return buildAuthorizer(nil, opts...)
}

// NewAuthz uses db for GORM policy storage, overriding the configured adapter.
// The application must create the policy table before calling NewAuthz.
// The caller owns db.
func NewAuthz(db *gorm.DB, opts ...Option) (*Authorizer, error) {
	if db == nil {
		return nil, errors.New("authz: nil database")
	}
	return buildAuthorizer(db, opts...)
}

// Authorize checks the subject, object and action against the current policy.
func (a *Authorizer) Authorize(sub, obj, act string) (bool, error) {
	return a.Enforce(sub, obj, act)
}

// Enforce checks model-specific request values using the synchronized enforcer.
func (a *Authorizer) Enforce(rvals ...any) (bool, error) {
	allowed, err := a.engine.Enforce(rvals...)
	if err != nil {
		return false, fmt.Errorf("authz: enforce: %w", err)
	}
	return allowed, nil
}

// LoadPolicy reloads policies through the synchronized enforcer.
func (a *Authorizer) LoadPolicy() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.engine.LoadPolicy(); err != nil {
		return fmt.Errorf("authz: reload policy: %w", err)
	}
	return nil
}

func buildAuthorizer(db *gorm.DB, opts ...Option) (*Authorizer, error) {
	options := NewOptions(opts...)
	if err := options.Validate(); err != nil {
		return nil, fmt.Errorf("authz: validate options: %w", err)
	}

	m, err := model.NewModelFromFile(options.modelFilePath)
	if err != nil {
		return nil, fmt.Errorf("authz: parse model: %w", err)
	}
	adapter := options.policyAdapter
	if db != nil {
		adapter, err = newGORMAdapter(db, options.policyLoadTimeout)
		if err != nil {
			return nil, fmt.Errorf("authz: create adapter: %w", err)
		}
	}
	// Casbin skips the initial load when IsFiltered is true. This wrapper has
	// no filtered-loading API, so returning such an enforcer would leave it empty.
	if filtered, ok := adapter.(persist.FilteredAdapter); ok && filtered.IsFiltered() {
		return nil, errors.New("authz: initial policy load: already filtered policy adapter is not supported")
	}
	engine, err := casbin.NewSyncedEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("authz: initial policy load: %w", err)
	}

	// Application services own policy writes; this authorizer reads committed
	// policies through LoadPolicy. Disable incremental persistence so in-memory
	// policy edits cannot bypass the application's policy management path.
	// Explicit SavePolicy and the adapter's schema migration are not affected.
	engine.EnableAutoSave(false)
	return &Authorizer{engine: engine}, nil
}

func newGORMAdapter(db *gorm.DB, timeout time.Duration) (*timedAdapter, error) {
	// Materialize the clean statement before TurnOffAutoMigrate calls WithContext;
	// otherwise that call replaces GORM's pending NewDB reset with a statement clone
	// and retains the caller's query conditions. The caller's DB stays untouched.
	policyDB := db.Session(&gorm.Session{
		NewDB:       true,
		Context:     context.Background(),
		Initialized: true,
	})
	gormadapter.TurnOffAutoMigrate(policyDB)
	adapter, err := gormadapter.NewAdapterByDB(policyDB)
	if err != nil {
		return nil, err
	}
	return &timedAdapter{Adapter: adapter, timeout: timeout}, nil
}
