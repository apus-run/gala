# job

One-shot workload runner: register Go funcs by name in an explicit registry,
trigger a single run from the CLI or over HTTP, and optionally guard that run
with a distributed lease so only one replica executes it. Standard library
only; the lease contract comes from the parent `task` package.

No global registry, no package-level state, no persistence, no scheduler — the
layer only turns a name plus a payload into exactly one function call.

## Usage

```go
r := job.NewRegistry(
	// Optional single-run lease: one fresh Locker per Run, one key per job.
	// 可选单次执行租约：每次 Run 一个新 Locker，每个任务一个锁键。
	job.WithLockerProvider(func(ctx context.Context, name string) (task.Locker, error) {
		return lockClient.NewLock(ctx,
			dlock.WithLockName("job:"+name),
			dlock.WithLockTimeout(30*time.Second),
		)
	}, job.WithLockRefreshInterval(2*time.Second)),
)

err := r.Register("resize", func(ctx context.Context) error {
	inv, ok := job.FromContext(ctx)
	if !ok {
		return errors.New("no invocation")
	}
	return resize(ctx, inv.Payload)
})

http.Handle("/jobs", r.Handler())                       // POST X-Gala-Job-Name: resize
err = r.RunCLI(ctx, []string{"--job", "resize", "--job-data", "-"}, os.Stdin)
```

`Register` rejects an empty name, a nil func and a duplicate name
(`ErrEmptyName`, `ErrNilFunc`, `ErrDuplicate`); it never replaces an existing
job. `Names()` returns a sorted snapshot. `Run` on an unknown name returns an
error wrapping `ErrNotFound`.

## Invocation

```go
type Invocation struct {
	Name    string
	RunID   string
	Payload []byte
	Header  http.Header
}
```

`Run` puts the invocation into the job's context; the job reads it with
`FromContext`. `Payload` and `Header` stay owned by the caller and are treated
as read-only. The invocation carries no business meaning: the application
decides what the payload and run id are.

## HTTP trigger

`Registry.Handler()` returns an `http.Handler` for one POST route:

| Condition | Result |
| --- | --- |
| non-POST method | 405, `Allow: POST` |
| missing/empty job header | 400 |
| unknown job (`errors.Is(err, ErrNotFound)`) | 404, checked before the body is read |
| request body over the limit | 413, the job does not run |
| failed to read the request body | 400 |
| job, lease or panic error | 500 (or the `WithHTTPErrorHandler` response) |
| success | 204 |

The request body is the payload; `X-Gala-Job-Name` names the job (override with
`WithJobHeader`); `X-Gala-Job-RunID` is copied into `Invocation.RunID` and
echoed in the response when present (override with `WithRunIDHeader`). All
request headers are copied into `Invocation.Header`. 500 bodies are generic on
purpose, so internal details and panic values never leak to clients — log with
middleware if needed.

The body is read through `http.MaxBytesReader` and capped at 10 MiB
(`defaultMaxBodyBytes`); `WithMaxBodyBytes(n)` changes the cap and a
non-positive `n` keeps the default. A body over the cap is refused with 413
before the job runs, so `Invocation.Payload` never exceeds it, and an unknown
job name is answered with 404 before the body is read at all.
`WithHTTPErrorHandler` is called for run errors and for an unknown job name,
not for those adapter-level rejections.

The adapter owns no server, holds no global registry and imposes no auth or
server-side timeout: wrap it with auth middleware or a router as needed.
Platform-specific protocols (GitHub webhooks, CloudEvents, signed requests,
per-job paths) belong in adapters that translate into this handler or call
`Registry.Run` directly.

## CLI trigger

`RunCLI(ctx, args, stdin)` parses a fresh `flag.FlagSet` named `job` and calls
`Run`:

- `--job` (required) — registered job name; missing returns an error wrapping
  `ErrEmptyName`.
- `--job-data` — payload as a string; `-` reads stdin when `stdin` is non-nil;
  empty means no payload.
- `--job-header` — semicolon-separated `key=value` pairs, each split on the
  first `=`, so values may contain `=`; keys may repeat.

`RunCLI` never prints, exits or logs; it returns errors for the caller to
report.

## Optional lease

`WithLockerProvider` takes a function returning the `task.Locker` interface
(structurally identical to `components/dlock.Locker`), so any dlock backend can
be adapted without this module importing `components/dlock`.

**One Locker per Run.** The provider is called on every `Run` (bounded by
`WithLockAcquireTimeout`) and must return a locker with its own ownership
token — `dlock`'s `Client.NewLock` does. Never return one cached Locker for
concurrent Runs: dlock's `Lock` is an idempotent extend for the same token, so
two overlapping Runs would both "hold" the lease and the first `Unlock` would
release it for the still-running other. The provider also chooses the lock key
(`"job:"+name` above), so different job names never exclude each other.
Returning `(nil, nil)` runs that call without a lease; a provider error fails
the call without running the job.

When a provider is configured, `Run`:

1. resolves the Locker and acquires the lease, each with a timeout bounded by
   `WithLockAcquireTimeout` (default 5s); if `Lock` fails, the job does not run
   and the error is wrapped with `%w`, so `errors.Is(err, dlock.ErrLocked)`
   still works;
2. refreshes on a ticker (`WithLockRefreshInterval`, default 5s) while the job
   runs; a refresh error while the job is still running is recorded, cancels
   the job's derived context and stops refreshing. A refresh aborted because
   `Run` is stopping is not an error;
3. after the job returns, cancels and waits for the refresher — aborting a
   refresh still in flight rather than waiting out its timeout — then unlocks
   with a fresh timeout context derived from the caller's values but not its
   cancellation (`context.WithoutCancel`), so a disconnected caller still
   releases the lease;
4. returns `errors.Join` of the job, refresh and unlock errors, ignoring nil —
   no failure is hidden.

Without a provider, `Run` calls the job directly with the caller's context.

## Semantics worth knowing

- **Panics are recovered.** `Run` returns a `*PanicError{Value, Stack}`, so an
  HTTP- or CLI-triggered job cannot crash the process. `Error()` contains only
  the panic value; read the stack with `errors.As` and log it.
- **Registration is fixed at startup.** There is no dynamic replacement:
  `Register` returns `ErrDuplicate` for an existing name, so a given name maps
  to exactly one function for the registry's lifetime.
- **Registries are independent.** Creating a second `Registry` does not see the
  first one's jobs; wiring (which jobs, which lock, which TTL) is the
  application's job.
- **Race-free by construction.** Register, Names and Run are safe for
  concurrent use; the lease timings are read once at construction.
- **Lease is per Run call, not a queue.** A busy lease returns an error instead
  of waiting; retry/backoff is the caller's policy. Two concurrent Runs of the
  same name are excluded by the backend only because each Run has its own
  Locker/token.

## Known limits

- No persistence, run history or retry policy. The registry knows nothing about
  past runs.
- The business owns idempotency: the lease excludes concurrent executions of a
  name across replicas only while an execution holds it; a caller can still
  trigger the same logical work twice (for example after a timeout or after the
  first run finished), and the second call runs.
- One-shot only — scheduling belongs to `components/task/cron`.
- The HTTP adapter is generic: no auth, only a whole-handler body cap (10 MiB
  by default, `WithMaxBodyBytes`), no server-side timeout and no per-job
  routes. Platform protocols belong in dedicated adapters.
- `PanicError.Error()` omits the stack trace; without `WithHTTPErrorHandler`
  (or a custom wrapper around `Registry.Run`), a 500 response leaves no
  server-side trace by itself.
