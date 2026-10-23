# cron

In-process crontab: register Go funcs by id with 5-field cron expressions,
and one ticker fires each due job in its own goroutine. Zero dependencies,
DST-correct, and a panicking job never crashes the process.

Not a distributed scheduler: no persistence and no coordination between
replicas — every process running the same `Cron` fires its own jobs.

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
  returns `ctx.Err()` and lets the rest keep running in the background.

A runnable ops example (heartbeat, panic recovery, timezone-pinned morning
brief, overlap guard, graceful shutdown): [examples/ops](examples/ops).

## Known limits

- Seconds-level scheduling is not expressible.
- Single process only: in a multi-replica deployment each replica fires the
  job, so an external lock or leader election is the caller's job.
- `Add` with an existing id replaces the job, but a run of the old version
  that is already in progress is not interrupted.
