//go:build integration && (linux || darwin)

package dlock_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/apus-run/gala/components/dlock"
)

func pauseDB(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), "postgres://dlock:dlock@"+dockerEndpoint(t, "DLOCK_POSTGRES_ADDR")+"/dlock?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}
func pauseClient(t *testing.T, backend, key string, parent bool) dlock.Client {
	t.Helper()
	if backend == "mongo" {
		return dockerMongoClient(t, key, parent)
	}
	return dockerClient(t, backend)
}
func pauseTTL(backend string) time.Duration {
	if backend == "consul" {
		return 10 * time.Second
	}
	return 4 * time.Second
}

// Executed only as a child of TestDockerLeasesPausedHolder. The kernel stops
// the whole process, including any goroutines, after its last successful renewal.
func TestDockerLeaseHolderProcess(t *testing.T) {
	key := os.Getenv("DLOCK_PAUSE_WORK_KEY")
	if key == "" {
		t.Skip("parent supplies a unique work item")
	}
	backend := dockerEndpoint(t, "DLOCK_PAUSE_BACKEND")
	ctx := t.Context()
	conn := pauseDB(t)
	l := faultLocker(t, pauseClient(t, backend, key, false), key, pauseTTL(backend))
	if err := l.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := conn.QueryRow(ctx, "SELECT version FROM dlock_pause_work WHERE id=$1", key).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := l.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	// Resume at the original instruction boundary, before another call to the
	// lock service. Only the business write can reject the obsolete version here.
	plain, err := conn.Exec(ctx, "UPDATE dlock_pause_work SET plain='old' WHERE id=$1", key)
	if err != nil || plain.RowsAffected() != 1 {
		t.Fatalf("plain update: %v, %v", plain, err)
	}
	guarded, err := conn.Exec(ctx, "UPDATE dlock_pause_work SET guarded='old',version=version+1 WHERE id=$1 AND version=$2", key, version)
	if err != nil || guarded.RowsAffected() != 0 {
		t.Fatalf("stale conditional update: %v, %v", guarded, err)
	}
	if err := l.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old Refresh: %v", err)
	}
	if err := l.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old Unlock: %v", err)
	}
}

func TestDockerLeasesPausedHolder(t *testing.T) {
	db := pauseDB(t)
	_, err := db.Exec(t.Context(), `CREATE TABLE IF NOT EXISTS dlock_pause_work (
		id TEXT PRIMARY KEY, version INT NOT NULL DEFAULT 0,
		plain TEXT NOT NULL DEFAULT 'initial', guarded TEXT NOT NULL DEFAULT 'initial')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"redis", "mysql", "postgres", "mongo", "etcd", "consul"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
			defer cancel()
			key := "pause-" + rand.Text()
			store := pauseDB(t)
			if _, err := store.Exec(ctx, "INSERT INTO dlock_pause_work(id) VALUES($1)", key); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = store.Exec(context.Background(), "DELETE FROM dlock_pause_work WHERE id=$1", key) })
			c := pauseClient(t, backend, key, true)
			if init, ok := c.(dlock.Initializer); ok {
				if err := init.Init(ctx); err != nil {
					t.Fatal(err)
				}
			}
			next := faultLocker(t, c, key, time.Minute)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDockerLeaseHolderProcess$", "-test.v")
			cmd.Env = append(os.Environ(), "DLOCK_PAUSE_BACKEND="+backend, "DLOCK_PAUSE_WORK_KEY="+key)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
				if t.Failed() {
					t.Log(output.String())
				}
			})
			awaitFaultState(t, "child reached SIGSTOP", 15*time.Second, func() error {
				var state syscall.WaitStatus
				pid, err := syscall.Wait4(cmd.Process.Pid, &state, syscall.WNOHANG|syscall.WUNTRACED, nil)
				if errors.Is(err, syscall.EINTR) {
					return err
				}
				if err != nil {
					t.Fatal(err)
				}
				if pid == 0 {
					return errors.New("waiting for child")
				}
				// WUNTRACED without WCONTINUED reports only stops and exits.
				// Go on Darwin labels a SIGSTOP status as Continued().
				if state.Exited() || state.Signaled() {
					t.Fatalf("child exited before pause: %v", state)
				}
				return nil
			})
			stopped := time.Now()
			if err := next.Lock(ctx); !errors.Is(err, dlock.ErrLocked) {
				t.Fatalf("admitted B before expiry: %v", err)
			}
			time.Sleep(pauseTTL(backend) + 100*time.Millisecond)
			awaitFaultState(t, "takeover while child remains stopped", 65*time.Second, func() error {
				err := next.Lock(ctx)
				if err != nil && !errors.Is(err, dlock.ErrLocked) {
					t.Fatalf("takeover: %v", err)
				}
				return err
			})
			update, err := store.Exec(ctx, "UPDATE dlock_pause_work SET plain='successor',guarded='successor',version=version+1 WHERE id=$1", key)
			if err != nil || update.RowsAffected() != 1 {
				t.Fatalf("successor write: %v, %v", update, err)
			}
			duration := time.Since(stopped)
			if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("resumed child: %v", err)
			}
			var plain, guarded string
			var version int
			if err := store.QueryRow(ctx, "SELECT plain,guarded,version FROM dlock_pause_work WHERE id=$1", key).Scan(&plain, &guarded, &version); err != nil {
				t.Fatal(err)
			}
			if plain != "old" || guarded != "successor" || version != 1 {
				t.Fatalf("unexpected final row: %s/%s@%d", plain, guarded, version)
			}
			if err := next.Refresh(ctx); err != nil {
				t.Fatalf("successor lease lost: %v", err)
			}
			if err := next.Unlock(ctx); err != nil {
				t.Fatal(err)
			}
			t.Logf("stopped %s (TTL %s); old plain write committed, stale conditional write rejected, successor lease intact", duration.Round(time.Millisecond), pauseTTL(backend))
		})
	}
}
