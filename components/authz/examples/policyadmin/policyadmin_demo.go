package policyadmin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/apus-run/gala/components/authz"
	gormadapter "github.com/apus-run/gala/components/authz/adapter/gorm"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"gorm.io/gorm"
)

// Run performs the write contract end to end on one policy store: the admin
// commits rows, SyncPolicy reloads its own Authorizer synchronously and hints
// peers, and a second Authorizer sharing the store converges through its
// watcher. It returns only after the peer reflects the final state.
func Run(ctx context.Context, db *gorm.DB, client redisadapter.WatcherClient, modelPath string) error {
	// Subscribe before any write: a watcher created after a publish would miss
	// the event, and the peer would wait for its periodic reload instead.
	writerWatcher, err := redisadapter.NewWatcher(ctx, client, "authz:policyadmin:changed")
	if err != nil {
		return err
	}
	defer writerWatcher.Close()
	peerWatcher, err := redisadapter.NewWatcher(ctx, client, "authz:policyadmin:changed")
	if err != nil {
		return err
	}
	defer peerWatcher.Close()

	// The application creates the policy table; NewAuthz never migrates.
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		return fmt.Errorf("policyadmin: create policy table: %w", err)
	}

	// One watcher per Authorizer: SetUpdateCallback binds a single callback.
	// The writer's watcher publishes on SyncPolicy; the peer's only receives.
	onError := func(err error) { log.Print(err) }
	az, err := authz.NewAuthz(db, authz.WithModelFilePath(modelPath),
		authz.WithWatcher(writerWatcher), authz.WithWatcherErrorHandler(onError))
	if err != nil {
		return err
	}
	peer, err := authz.NewAuthz(db, authz.WithModelFilePath(modelPath),
		authz.WithWatcher(peerWatcher), authz.WithWatcherErrorHandler(onError))
	if err != nil {
		return err
	}

	admin := New(db, az)
	allowed := func(a *authz.Authorizer, sub, obj, act string) bool {
		ok, err := a.Enforce(sub, obj, act)
		return err == nil && ok
	}

	// Grant: role assignment plus permissions.
	if err := admin.AddPolicy(ctx, "editor", "/articles/*", "GET"); err != nil {
		return err
	}
	if err := admin.AddRoleForUser(ctx, "alice", "editor"); err != nil {
		return err
	}
	if err := admin.AddPolicy(ctx, "viewer", "/reports/archive", "GET"); err != nil {
		return err
	}
	if err := admin.AddPolicy(ctx, "viewer", "/reports/list", "GET"); err != nil {
		return err
	}
	if err := admin.AddRoleForUser(ctx, "alice", "viewer"); err != nil {
		return err
	}
	// SyncPolicy reloaded the writer's own Authorizer, so these are synchronous.
	if !allowed(az, "alice", "/articles/1", "GET") {
		return errors.New("policyadmin: alice should reach /articles/1")
	}
	if !allowed(az, "alice", "/reports/archive", "GET") {
		return errors.New("policyadmin: alice should reach /reports/archive")
	}

	// Revoke one rule; the sibling rule survives.
	if err := admin.RemovePolicy(ctx, "viewer", "/reports/archive", "GET"); err != nil {
		return err
	}
	if allowed(az, "alice", "/reports/archive", "GET") {
		return errors.New("policyadmin: /reports/archive should be denied")
	}
	if !allowed(az, "alice", "/reports/list", "GET") {
		return errors.New("policyadmin: /reports/list should still be allowed")
	}

	// Revoke one role assignment; the viewer role and its rules survive, so
	// reassigning restores access immediately.
	if err := admin.RemoveRoleForUser(ctx, "alice", "viewer"); err != nil {
		return err
	}
	if allowed(az, "alice", "/reports/list", "GET") {
		return errors.New("policyadmin: alice should have lost the viewer role")
	}
	if err := admin.AddRoleForUser(ctx, "alice", "viewer"); err != nil {
		return err
	}
	if !allowed(az, "alice", "/reports/list", "GET") {
		return errors.New("policyadmin: reassigning the role should restore access")
	}

	// Decommission a subject: every p rule with v0 = viewer disappears at once.
	if err := admin.DeletePermissionsForUser(ctx, "viewer"); err != nil {
		return err
	}
	if allowed(az, "alice", "/reports/list", "GET") {
		return errors.New("policyadmin: viewer permissions should be gone")
	}
	if !allowed(az, "alice", "/articles/1", "GET") {
		return errors.New("policyadmin: editor permission should survive")
	}

	// SyncPolicy success is not an acknowledgement from peers; wait for the
	// peer the way a read-your-writes caller would.
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return waitForFinalPolicy(waitCtx, peer.Enforce)
}

// waitForFinalPolicy checks both retained grants and completed revocations.
func waitForFinalPolicy(ctx context.Context, enforce func(...any) (bool, error)) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("policyadmin: peer authorizer did not converge: %w", err)
		}
		converged := true
		for _, check := range []struct {
			path string
			want bool
		}{
			{"/articles/1", true}, {"/reports/list", false}, {"/reports/archive", false},
		} {
			ok, err := enforce("alice", check.path, "GET")
			if err != nil {
				return fmt.Errorf("policyadmin: check peer policy: %w", err)
			}
			if ok != check.want {
				converged = false
			}
		}
		if converged {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("policyadmin: peer authorizer did not converge: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
