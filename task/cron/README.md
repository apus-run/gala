# cron

In-process crontab: register Go funcs by id with 5-field cron expressions,
and one ticker fires each due job in its own goroutine. Zero dependencies,
DST-correct, and a panicking job never crashes the process.

By default it is not a distributed scheduler: no persistence and no
coordination between replicas — every process running the same `Cron` fires
its own jobs. An optional per-job leader lease (`WithLockerProvider`) makes
replicas coordinate each due fire; see [Distributed](#distributed).

## Usage

```go
c := cron.New(cron.WithLocation(time.UTC))
c.MustAdd("dailyReport", "0 0 * * *", func() { ... }, cron.SkipIfRunning())
c.MustAdd("brief", "CRON_TZ=Asia/Shanghai 0 9 * * MON-FRI", func() { ... })
c.Start()
defer c.Shutdown(context.Background()) // stops the ticker, waits for the running jobs
```

`Add` returns an error for an invalid expression or empty id (`MustAdd`
panics instead); `Jobs()` snapshots ids, expressions and next run times;
`Remove`/`RemoveAll`/`SetInterval`/`SetTimezone` reconfigure a live cron.

## Semantics worth knowing

- **Minute granularity.** Expressions are 5 fields + macros (`@daily`,
  `@hourly`, ...); the tick interval is capped at 1m and shorter ticks are
  deduplicated to once per wall-clock minute.
- **DST-safe.** A minute that doesn't exist (spring forward) is skipped; a
  minute repeated by a fall-back runs only once.
- **`CRON_TZ=` prefix** pins a job's timezone, independent of the Cron's
  location. Programs that may run without a system tz database should
  `import _ "time/tzdata"`.
- **Day fields follow the Vixie rule:** either field a wildcard → both must
  match (AND); both restricted → either matches (OR).
- **Every run gets its own goroutine** — a slow job never delays the others.
  `SkipIfRunning()` prevents a job from overlapping its previous run
  (including across an Add that replaces it).
- **Panics go to `PanicHandler`** (default: log with stack). Re-panic from a
  custom handler to crash the process.
- **`Shutdown(ctx)`** stops the ticker and waits for in-flight jobs, or
  returns `ctx.Err()` and lets the rest keep running in the background. With
  leases, one shared background waiter still releases every lease once the
  in-flight jobs finish, even after `ctx` expired.

A runnable ops example (heartbeat, panic recovery, timezone-pinned morning
brief, overlap guard, graceful shutdown): [examples/ops](examples/ops).

## Distributed

Every replica keeps every job registered. With `WithLockerProvider`, each due
fire first has to win a per-job leader lease before the job runs. Once a
replica becomes leader for a job it keeps the lease and refreshes it in the
background across fires, so a delayed tick on another replica cannot cause a
duplicate run.

```go
c := cron.New(cron.WithLockerProvider(
	func(ctx context.Context, jobID string) (task.Locker, error) {
		// One Locker per replica and job. Adapt your backend here; the
		// cron package itself never imports components/dlock.
		return dlockClient.NewLock(ctx,
			dlock.WithLockName("cron:"+jobID),
			dlock.WithLockTimeout(30*time.Second),
			dlock.WithOwnerID(hostname),
		)
	},
	cron.WithLockRefreshInterval(10*time.Second), // must be well below the TTL
	cron.WithLockAcquireTimeout(5*time.Second),
	cron.WithLockEventHandler(func(jobID string, event cron.LockEvent, err error) {
		// Called synchronously from job/refresher/Stop goroutines: keep it
		// fast, concurrency-safe and never block or call back into Cron.
		log.Printf("cron: %s lease %s: %v", jobID, event, err)
	}),
))

c.MustAdd("dailyReport", "0 0 * * *", func() { ... }, cron.SkipIfRunning())
c.Start()
defer c.Shutdown(context.Background())
```

The adapter sketch above is README code: the `task` module depends only on
`github.com/apus-run/gala/task` (which holds the `Locker` interface), so an
application wires in `components/dlock` (or any other backend) itself. The
returned `Locker` must carry this replica's own ownership token — never share
one `Locker` between replicas.

**Provider semantics.** The provider is called lazily, on the first due fire
of a job; `New`, `Add` and `Start` do no I/O. Returning `(nil, nil)` marks
the job local-only: every replica runs it and the provider is never called
again. A provider error is not cached: that fire is skipped, `LockProviderFailed`
is emitted, and the next due fire retries.

**Events.** `LockEventHandler` receives:

| Event | Meaning | Reaction |
| --- | --- | --- |
| `LockAcquireFailed` | `Lock` errored (typically held by another replica) | This fire is skipped |
| `LockProviderFailed` | The provider errored | This fire is skipped; retried next fire |
| `LockLost` | `Refresh` failed while leader | Demoted; the lease is deliberately **not** released (it may still be healthy), so the next fire may re-`Lock` |
| `LockReleaseFailed` | `Unlock` errored, including "not held" | Informational; the package does not import dlock and cannot classify it |

All lease calls (provider, `Lock`, `Refresh`, `Unlock`) are bounded by the
acquire timeout. `Stop`/`Shutdown` releases share one overall acquire-timeout
budget and run concurrently, so N leases do not cost N timeouts. A leader that
loses its lease is demoted and its refresher stops; `Unlock` is attempted only
when the lease may still be held.

**Stop vs Shutdown.** `Stop` stops the ticker and releases every lease
immediately, without waiting for running jobs (another replica may take over
while they finish), using that single overall budget and concurrent releases.
It also fences every fire dispatched before it that has not passed the lease
gate yet: they are skipped, or release the lease they just acquired, instead
of restarting a refresher a stopped Cron would keep alive. The fence is an
epoch that increments on every `Stop`: restarting the ticker (`Start`,
`SetInterval`) never releases leases and opens the gate for new fires, but a
fire dispatched before `Stop` stays fenced and can neither run nor acquire. A
release that was delayed across a concurrent `Start` is tied to the leader
generation it snapshotted (each successful `Lock` bumps it), so it never
cancels the refresher of, or Unlocks, a lease the restarted Cron acquired in
the meantime. A `Stop` also aborts an in-flight `Refresh` immediately instead
of waiting for the acquire timeout; an aborted refresh is a shutdown, not a
lost lease, and is not reported as `LockLost`.

`Shutdown(ctx)` stops only the ticker, then one shared background waiter drains
the loops and in-flight jobs and releases every lease (same single budget and
concurrency as `Stop`). While it runs, every `Shutdown` call shares that waiter
instead of leaking one goroutine each, and it keeps running even if `ctx`
expires: a timed-out `Shutdown` returns `ctx.Err()` with the leases intact
while the job runs, then releases them as soon as the job finishes, so no later
`Shutdown` is needed. Once the waiter has completed, the next `Shutdown` starts
a fresh one, so a Cron restarted with `Start` (for example after a `Stop` before
the first `Start`) is drained and its leases released again.

`Remove`/`RemoveAll` tombstone the affected states under the Cron lock and
release them asynchronously with one budget, returning without waiting for
`Unlock`. The tombstone keeps fencing the id until the release finishes: a fire
already dispatched for a removed id is skipped instead of acquiring (if its
`Lock` was already in flight, the post-`Lock` fence check releases it). A
successful release forgets the state, or reopens it when the id was re-added
meanwhile, so re-adding starts a fresh lifecycle (the state object and its lock
are reused, so a stale release still finishes before a new acquire). A failed
`Unlock` keeps the tombstone, the state, and its cached `Locker` (with this
replica's ownership token) so a later `Stop`/`Shutdown` retries it; the re-added
id stays fenced until that retry succeeds. `Add` with an existing id preserves
the job's lease, so a replacement stays leader; a fire dispatched for the old
version that has not reached the gate yet is dropped, and the next due fire
runs the new version with the leadership already held. A provider returning
`(nil, nil)` marks the id local-only, and that fire is still dropped if `Add`
replaced the job while the provider was in flight.

## Lifecycle

`cron.NewTask(c)` adapts a Cron to the `Start(context.Context) error` /
`Stop(context.Context) error` contract used by the root `gala.WithTasks`
lifecycle. `Start` starts the ticker; `Stop` delegates to `Shutdown(ctx)`, so
it waits for in-flight jobs (bounded by ctx) and then releases every lease. A
`Stop` before the first `Start` is safe, and a later `Start` is drained by the
next `Stop`. A nil `Cron` is reported by `Start` and ignored by `Stop` instead
of panicking.

## Known limits

- Seconds-level scheduling is not expressible.
- Without `WithLockerProvider`, every replica fires its own jobs, so an
  external lock or leader election is the caller's job.
- **No exactly-once.** The lease narrows the window to at most one leader at a
  time; a fire is skipped by the follower and is not replayed.
- **Failover gap up to the lease TTL.** After a leader dies without
  `Stop`/`Shutdown`, the job does not run until the backend lease expires
  (and a replica gets a due fire).
- **No fencing token.** A leader that lost its lease (eg. a long GC pause)
  keeps running its current job, because job funcs take no context and are
  never cancelled on lease loss. Keep such jobs idempotent or bound their
  runtime well below the TTL.
- The refresher keeps a lost-lease job's lease in "may be held" state and does
  not call `Unlock`; a later `Stop`/`Shutdown` still attempts it once.
- `Remove`/`RemoveAll` return before their `Unlock` completes; a failed or
  budget-expired release keeps the tombstone, state, and cached `Locker` until a
  later `Stop`/`Shutdown` retries it.
- `Add` with an existing id replaces the job, but a run of the old version
  that is already in progress is not interrupted.
- `Stop` releases leases without waiting for in-flight jobs, so another
  replica may take one over while the stopped replica's job is still running.
  Fires that never passed the lease gate are fenced; fires that did are not
  cancelled (job funcs take no context).
