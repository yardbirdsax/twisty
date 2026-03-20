---
# Task 013: Update termProgressBar to Display Retry and Duration Stats

## Summary

Replace the stub `Retry()` and `FetchDuration()` methods on `termProgressBar` with real implementations that track total retry count, last fetch duration, and average fetch duration. Update the rendered progress line to display these stats.

## Dependencies

Task 012 — the interface must have `Retry()` and `FetchDuration(time.Duration)` before this task can be implemented.

## Detailed Directions

### 1. Add Fields to termProgressBar

In `main.go`, extend the `termProgressBar` struct with four new fields:

```go
type termProgressBar struct {
    // existing fields
    total   int
    current int
    cached  int
    fetched int
    w       io.Writer

    // new fields
    retries           int
    lastFetchDuration time.Duration
    totalFetchDuration time.Duration
    fetchCount        int
}
```

### 2. Implement Retry()

Increment the retry counter:

```go
func (b *termProgressBar) Retry() {
    b.retries++
    b.render()
}
```

Calling `render()` here allows the retry count to update immediately without waiting for a `Tick()`, giving the user live feedback during a slow or retrying tile.

### 3. Implement FetchDuration()

Record the duration and update the running total for the average:

```go
func (b *termProgressBar) FetchDuration(d time.Duration) {
    b.lastFetchDuration = d
    b.totalFetchDuration += d
    b.fetchCount++
}
```

Do not call `render()` here — `Tick()` is always called after `FetchDuration()` for network fetches, so the updated values will be visible on the next render.

### 4. Update the render() Method

Extend the stats portion of the progress line. The new format is:

```
Fetching tiles: [======>  ] 5/12 (3 cached, 2 fetched, 1 retry, last: 8.3s, avg: 6.1s)
```

Rules:
- Show `N retry` / `N retries` only when `retries > 0`.
- Show `last: X.Xs` only when at least one fetch has completed (`fetchCount > 0`).
- Show `avg: X.Xs` only when at least one fetch has completed (`fetchCount > 0`).
- Format durations with one decimal place in seconds: use `fmt.Sprintf("%.1fs", d.Seconds())`.
- Use singular "retry" when `retries == 1`, plural "retries" otherwise.

Example render logic for the stats suffix:

```go
func (b *termProgressBar) statsString() string {
    parts := []string{
        fmt.Sprintf("%d cached", b.cached),
        fmt.Sprintf("%d fetched", b.fetched),
    }
    if b.retries > 0 {
        noun := "retries"
        if b.retries == 1 {
            noun = "retry"
        }
        parts = append(parts, fmt.Sprintf("%d %s", b.retries, noun))
    }
    if b.fetchCount > 0 {
        avg := time.Duration(int64(b.totalFetchDuration) / int64(b.fetchCount))
        parts = append(parts, fmt.Sprintf("last: %.1fs", b.lastFetchDuration.Seconds()))
        parts = append(parts, fmt.Sprintf("avg: %.1fs", avg.Seconds()))
    }
    return strings.Join(parts, ", ")
}
```

### 5. Update Tests

In the test file for the progress bar (if one exists under `quality/` or in `main_test.go`), add cases covering:
- Retry count display (singular and plural).
- Duration display after one and multiple fetches.
- Correct omission of retry/duration fields when no retries or fetches have occurred.

## Acceptance Criteria

- [ ] Progress line shows retry count when retries > 0, omits it otherwise.
- [ ] Progress line shows `last:` and `avg:` duration when at least one fetch has completed, omits them otherwise.
- [ ] `Retry()` causes an immediate re-render so the count updates live.
- [ ] Duration values are formatted to one decimal place in seconds.
- [ ] Singular/plural "retry"/"retries" is used correctly.
- [ ] `go build ./...` and `go test ./...` pass.

## Notes

- The progress bar writes to stderr, not stdout, so it does not interfere with piped output.
- Keep the overall line length in mind — on narrow terminals the line may wrap. This is acceptable for now; no truncation logic is required.
- `FetchDuration` is intentionally not re-rendering — the `Tick()` call that follows will pick up the new values.

---
# Task 013 Review: Update termProgressBar to Display Retry and Duration Stats

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task replaced stub `Retry()` and `FetchDuration()` methods on `termProgressBar` with real implementations that track retries, last fetch duration, and average fetch duration. The rendered progress line was extended to display these stats conditionally.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| Progress line shows retry count when retries > 0, omits it otherwise | PASS |
| Progress line shows `last:` and `avg:` when at least one fetch completed, omits otherwise | PASS |
| `Retry()` causes an immediate re-render | PASS |
| Duration values formatted to one decimal place in seconds | PASS |
| Singular/plural "retry"/"retries" used correctly | PASS |
| `go build ./...` and `go test ./...` pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Conditional rendering omissions:** Stats fields are conditionally included only when meaningful, keeping the progress line clean at the start of a fetch run.

---

## Verification Commands Run

```bash
make test   # all packages pass
make lint   # no issues (go vet clean)
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. Implementation matches the spec exactly, tests cover all required cases (singular/plural retries, duration display, omission cases, accumulation correctness), and the full test suite passes cleanly.

