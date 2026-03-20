---
# Task 014: Wire Retry and Duration Events into the Fetch Pipeline

## Summary

Instrument `FetchTiledWays` in `quality/tilefetch.go` to call `cfg.Progress.Retry()` on each retry attempt and `cfg.Progress.FetchDuration(d)` after each HTTP round-trip, so the progress bar introduced in Task 013 receives live data.

## Dependencies

- Task 012 — interface must expose `Retry()` and `FetchDuration(time.Duration)`.
- Task 013 — `termProgressBar` must handle those calls (stubs from 012 are sufficient to unblock this task, but the full display requires 013).

## Detailed Directions

### 1. Locate the Retry Loop in FetchTiledWays

In `quality/tilefetch.go`, the per-tile fetch is wrapped by `retryWithBackoff`. The call looks roughly like:

```go
err := retryWithBackoff(ctx, 3, cfg.RetryDelay, func() error {
    raw, e = fetchTileRaw(ctx, cfg.Endpoint, tile)
    return e
})
```

### 2. Time Each HTTP Attempt

Wrap `fetchTileRaw` with a wall-clock timer and report the duration via `FetchDuration`. This should happen inside the closure passed to `retryWithBackoff`, after every call to `fetchTileRaw` regardless of success or failure:

```go
err := retryWithBackoff(ctx, 3, cfg.RetryDelay, func() error {
    start := time.Now()
    raw, e = fetchTileRaw(ctx, cfg.Endpoint, tile)
    cfg.Progress.FetchDuration(time.Since(start))
    return e
})
```

### 3. Report Retries

`retryWithBackoff` currently does not notify the caller about individual attempts. To report retries without changing the retry function's signature, add an attempt counter inside the closure:

```go
attempt := 0
err := retryWithBackoff(ctx, 3, cfg.RetryDelay, func() error {
    if attempt > 0 {
        cfg.Progress.Retry()
    }
    attempt++
    start := time.Now()
    raw, e = fetchTileRaw(ctx, cfg.Endpoint, tile)
    cfg.Progress.FetchDuration(time.Since(start))
    return e
})
```

`Retry()` is called before the HTTP attempt (so the counter increments immediately when a retry begins), while `FetchDuration()` is called after (so the duration reflects the completed attempt).

### 4. Update Integration Tests

In `quality/tilefetch_test.go` (or equivalent integration test file), add assertions that verify `Retry()` and `FetchDuration()` are called the expected number of times under retry scenarios. Use a test double (spy) that implements `ProgressReporter`:

```go
type spyProgress struct {
    NoopProgressReporter
    retries   int
    durations []time.Duration
}

func (s *spyProgress) Retry()                        { s.retries++ }
func (s *spyProgress) FetchDuration(d time.Duration) { s.durations = append(s.durations, d) }
```

Scenarios to cover:
- Single successful fetch (no retries): `Retry()` called 0 times, `FetchDuration()` called once.
- One retry before success: `Retry()` called 1 time, `FetchDuration()` called twice.
- Cache hit: `Retry()` called 0 times, `FetchDuration()` called 0 times.

## Acceptance Criteria

- [ ] `cfg.Progress.Retry()` is called once per retry attempt (not on the initial attempt).
- [ ] `cfg.Progress.FetchDuration(d)` is called once per HTTP attempt, success or failure.
- [ ] Cache hits do not trigger `Retry()` or `FetchDuration()`.
- [ ] Integration tests cover the scenarios listed above and pass.
- [ ] `go build ./...` and `go test ./...` pass.

## Notes

- `time.Since(start)` measures wall-clock time including network latency, Overpass processing, and any OS scheduling delays. This is intentional — the user wants to know how long the fetch actually took.
- Do not measure the retry backoff sleep as part of the fetch duration; the timer wraps only the `fetchTileRaw` call.
- The `attempt` variable closure approach avoids any changes to the `retryWithBackoff` signature, keeping that function general-purpose.

---

# Task 014 Review: Wire Retry and Duration Events into the Fetch Pipeline

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task instruments `FetchTiledWays` to call `cfg.Progress.Retry()` on each retry attempt and `cfg.Progress.FetchDuration(d)` after each HTTP round-trip, and adds integration tests covering single success, one-retry-then-success, and cache-hit scenarios.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `cfg.Progress.Retry()` is called once per retry attempt (not on the initial attempt) | PASS |
| `cfg.Progress.FetchDuration(d)` is called once per HTTP attempt, success or failure | PASS |
| Cache hits do not trigger `Retry()` or `FetchDuration()` | PASS |
| Integration tests cover the three specified scenarios and pass | PASS |
| `go build ./...` and `go test ./...` pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Thread-safe spy:** `retrySpyProgress` uses a mutex to protect `retries` and `durations`, preventing data races in test scenarios where the progress reporter could theoretically be called concurrently.

---

## Verification Commands Run

```bash
make test   # all packages pass
make lint   # go vet passes, no issues reported
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, the implementation matches the spec exactly, and all tests pass with no lint issues.
