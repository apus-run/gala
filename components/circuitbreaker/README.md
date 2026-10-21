# circuitbreaker

Transport-agnostic circuit breaker wrapping [gobreaker v2](https://github.com/sony/gobreaker)'s
two-step breaker, adding the operations surface the
[ginx middleware](../ginx/middlewares/circuitbreaker) keeps: force controls
(`ForceOpen`/`ForceClose`/`Reset`) and cumulative metrics (`Stats`). It exists so
other transports can build middleware with comparable capability, and so the two
implementations can be compared on the same workload shapes.

## Usage

```go
b := circuitbreaker.New(gobreaker.Settings{
    Name:        "orders",
    MaxRequests: 1,              // half-open probes; also the close threshold
    Interval:    time.Minute,    // closed-state count window
    Timeout:     30 * time.Second,
    ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 5 },
})

done, err := b.Allow()
if err != nil {
    // circuitbreaker.ErrOpenState -> 503, circuitbreaker.ErrTooManyRequests -> 429
    return
}
defer func() {
    if v := recover(); v != nil { // panics count as failures
        done(fmt.Errorf("panic: %v", v))
        panic(v)
    }
}()
done(work()) // nil error is a success
```

A Gin middleware with the same capability as the ginx one:
[examples/ginbreaker](examples/ginbreaker). For outbound calls use gobreaker's
`CircuitBreaker[T].Execute` directly; wrapping it adds nothing.

## Known upstream limits

- **No injectable clock**: tests use short `Timeout` values and sleep, unlike
  the ginx implementation whose fake clock drives every state instantly.
- **Close threshold is `MaxRequests`**: half-open requires every probe to
  succeed before closing; a separate success threshold is not expressible.
- **No count reset**: `Reset` only releases force modes; the upstream window
  keeps its history.
- The distributed/redis mode of gobreaker v2 serializes every execution through
  a global lock (cluster-wide concurrency of 1) and fails closed on redis
  outages. It is deliberately not exposed here; request-path breakers must stay
  local.

## Benchmarks

`go test -bench . -benchmem` — `BenchmarkGobreakerDirect` is the unwrapped
upstream; its gap to `BenchmarkBreakerClosed` is this package's overhead.
Numbers are not directly comparable with the ginx middleware's bench, which
drives whole Gin requests.
