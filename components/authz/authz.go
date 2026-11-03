// Package authz makes authorization decisions through a synchronized Casbin
// v3 enforcer with serialized policy reloads | 通过同步的 Casbin v3 执行器做授权判定，策略重载串行执行。
//
// # Notes | 说明
//
// An [Authorizer] is read-side only: auto-save is disabled, the application
// owns policy writes, and this package reloads committed policy via
// LoadPolicy, SyncPolicy or RunPolicyReload. With WithMaxPolicyAge set,
// checks return ErrPolicyStale once policy is too old — callers must deny.
// Adapters whose policy is already filtered are rejected at construction.
// [Authorizer] 只做读侧：自动保存已关闭，策略写入归应用，本包通过
// LoadPolicy、SyncPolicy 或 RunPolicyReload 重载已提交的策略。设置
// WithMaxPolicyAge 后策略过旧时检查返回 ErrPolicyStale，调用方必须拒绝。
// 已处于过滤状态的适配器在构造时会被拒绝。
//
// # Features | 功能
//
// - Authorize/Enforce and role queries on a SyncedEnforcer | 基于 SyncedEnforcer 的 Authorize/Enforce 与角色查询
// - Policy storage: file by default, GORM via NewAuthz, Redis via NewRedis, or any adapter via WithPolicyAdapter | 策略存储：默认文件，NewAuthz 用 GORM，NewRedis 用 Redis，也可用 WithPolicyAdapter 注入
// - Watcher-driven reloads plus a periodic RunPolicyReload loop | Watcher 触发的重载与周期性 RunPolicyReload 循环
//
// # Example | 示例
//
//	a, err := NewAuthz(db, WithModelFilePath("model.conf"))
//	ok, err := a.Authorize("alice", "data1", "read")
package authz

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	gormadapter "github.com/apus-run/gala/components/authz/adapter/gorm"
	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"

	"gorm.io/gorm"
)

// Authorizer provides synchronized authorization and serialized policy reloads.
// Do not copy an instance after use.
// Authorizer 提供同步的授权判定与串行的策略重载。
// 实例使用后不要复制。
type Authorizer struct {
	engine       *casbin.SyncedEnforcer
	mu           sync.Mutex
	watcher      persist.Watcher
	maxPolicyAge time.Duration
	lastLoad     atomic.Pointer[time.Time]
}

// timedAdapter bounds the database part of each policy load, including the
// constructor's initial load. It does not promise to cancel Casbin's CPU work.
// gorm-adapter's ordinary LoadPolicy uses context.Background, so putting a
// deadline on the GORM session alone would not bound that load.
// timedAdapter 为每次策略加载的数据库部分设置超时，含构造时的首次加载；
// 它不承诺取消 Casbin 的 CPU 计算。gorm-adapter 的普通 LoadPolicy 用的是
// context.Background，因此只在 GORM session 上加 deadline 无法约束那次加载。
type timedAdapter struct {
	*gormadapter.Adapter
	timeout time.Duration
}

// LoadPolicy loads policy with a bounded database timeout | LoadPolicy 在限时的 context 下加载策略.
func (a *timedAdapter) LoadPolicy(m model.Model) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	return a.Adapter.LoadPolicyCtx(ctx, m)
}

// New creates an initialized enforcer using the configured policy adapter.
// Adapters with an already filtered policy are not supported.
// The caller owns injected dependencies and policy synchronization tasks.
// New 用配置好的策略适配器创建已初始化的执行器。
// 不支持策略已处于过滤状态的适配器。
// 注入的依赖与策略同步任务由调用方持有。
func New(opts ...Option) (*Authorizer, error) {
	return buildAuthorizer(nil, opts...)
}

// NewAuthz uses db for GORM policy storage, overriding the configured adapter.
// The application must create the policy table before calling NewAuthz.
// The caller owns db.
// NewAuthz 用 db 做 GORM 策略存储，覆盖已配置的适配器。
// 调用 NewAuthz 前应用必须先建好策略表。
// db 由调用方持有。
func NewAuthz(db *gorm.DB, opts ...Option) (*Authorizer, error) {
	if db == nil {
		return nil, errors.New("authz: nil database")
	}
	return buildAuthorizer(db, opts...)
}

// Authorize checks the subject, object and action against the current policy | Authorize 按当前策略校验主体、客体与动作。
func (a *Authorizer) Authorize(sub, obj, act string) (bool, error) {
	return a.Enforce(sub, obj, act)
}

// Enforce checks model-specific request values using the synchronized enforcer | Enforce 用同步执行器校验模型相关的请求值。
func (a *Authorizer) Enforce(rvals ...any) (bool, error) {
	if err := a.policyFresh(); err != nil {
		return false, err
	}
	allowed, err := a.engine.Enforce(rvals...)
	if err != nil {
		return false, fmt.Errorf("authz: enforce: %w", err)
	}
	return allowed, nil
}

// LoadPolicy reloads policies through the synchronized enforcer | LoadPolicy 通过同步执行器重载策略。
func (a *Authorizer) LoadPolicy() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	started := time.Now()
	if err := a.engine.LoadPolicy(); err != nil {
		return fmt.Errorf("authz: reload policy: %w", err)
	}
	a.lastLoad.Store(&started)
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
	// IsFiltered 为 true 时 Casbin 会跳过首次加载；本封装没有过滤加载 API，
	// 返回这种执行器会让它一直是空的。
	if filtered, ok := adapter.(persist.FilteredAdapter); ok && filtered.IsFiltered() {
		return nil, errors.New("authz: initial policy load: already filtered policy adapter is not supported")
	}
	started := time.Now()
	engine, err := casbin.NewSyncedEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("authz: initial policy load: %w", err)
	}

	// Application services own policy writes; this authorizer reads committed
	// policies through LoadPolicy. Disable incremental persistence so in-memory
	// policy edits cannot bypass the application's policy management path.
	// Explicit SavePolicy and the adapter's schema migration are not affected.
	// 策略写入归应用服务，本授权器只通过 LoadPolicy 读已提交的策略。
	// 关闭增量持久化，避免内存里的策略改动绕过应用的策略管理路径。
	// 显式 SavePolicy 与适配器的建表迁移不受影响。
	engine.EnableAutoSave(false)
	a := &Authorizer{engine: engine, watcher: options.watcher, maxPolicyAge: options.maxPolicyAge}
	a.lastLoad.Store(&started)
	if a.watcher != nil {
		onError := options.watcherErrorHandler
		if err := a.watcher.SetUpdateCallback(func(string) {
			if err := a.LoadPolicy(); err != nil {
				onError(err)
			}
		}); err != nil {
			return nil, fmt.Errorf("authz: register watcher: %w", err)
		}
		// Catch changes committed after the first snapshot but before registration.
		// 补上首次快照之后、注册回调之前提交的变更。
		if err := a.LoadPolicy(); err != nil {
			// The caller still owns the watcher and must close it on construction failure.
			// 构造失败时 watcher 仍归调用方持有，须由调用方关闭。
			return nil, fmt.Errorf("authz: watcher catch-up: %w", err)
		}
	}
	return a, nil
}

func newGORMAdapter(db *gorm.DB, timeout time.Duration) (*timedAdapter, error) {
	// Materialize the clean statement before TurnOffAutoMigrate calls WithContext;
	// otherwise that call replaces GORM's pending NewDB reset with a statement clone
	// and retains the caller's query conditions. The caller's DB stays untouched.
	// 必须在 TurnOffAutoMigrate 调用 WithContext 之前物化干净的 statement，
	// 否则该调用会用 statement 克隆顶掉 GORM 待处理的 NewDB 重置，
	// 残留调用方的查询条件。调用方的 DB 不受影响。
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
