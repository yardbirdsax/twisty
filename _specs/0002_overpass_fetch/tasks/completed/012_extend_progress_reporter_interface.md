---
# Task 012: Extend ProgressReporter Interface with Retry and Duration Events

## Summary

Add two new methods to the `ProgressReporter` interface — `Retry()` and `FetchDuration(d time.Duration)` — so the fetch pipeline can report retry attempts and per-tile fetch durations. Update `NoopProgressReporter` with no-op implementations of both methods.

## Dependencies

Task 011 (completed) — the existing interface and no-op implementation live in `quality/progress.go`.

## Detailed Directions

### 1. Add Methods to the ProgressReporter Interface

In `quality/progress.go`, extend the interface with two new methods:

```go
type ProgressReporter interface {
    SetTotal(n int)
    Tick(cached bool)
    Retry()                          // called once per retry attempt (not the initial attempt)
    FetchDuration(d time.Duration)   // called once per successful or failed fetch with elapsed wall time
    Done()
}
```

`Retry()` is called each time the retry loop makes an additional attempt (i.e., not on the first try). If a tile exhausts all retries and fails, `Retry()` is still called for each retry attempt that was made.

`FetchDuration(d time.Duration)` is called after each call to `fetchTileRaw` completes (success or failure), with the wall-clock duration of that single HTTP round-trip. It is NOT called for cache hits.

### 2. Update NoopProgressReporter

Add no-op implementations of both methods to `NoopProgressReporter`:

```go
func (NoopProgressReporter) Retry()                        {}
func (NoopProgressReporter) FetchDuration(_ time.Duration) {}
```

### 3. Fix Compile Errors

After updating the interface, the compiler will flag any other types that implement `ProgressReporter` (e.g., `termProgressBar` in `main.go`). Add stub implementations there so the build is green before wiring work begins:

```go
func (b *termProgressBar) Retry()                        {}
func (b *termProgressBar) FetchDuration(_ time.Duration) {}
```

These stubs will be replaced in Task 013.

## Acceptance Criteria

- [ ] `ProgressReporter` interface has `Retry()` and `FetchDuration(time.Duration)` methods.
- [ ] `NoopProgressReporter` compiles with no-op implementations.
- [ ] `termProgressBar` (and any other implementors) compile with stub implementations.
- [ ] `go build ./...` passes with no errors.
- [ ] Existing tests pass unchanged (`go test ./...`).

## Notes

- Do not wire the new methods into the fetch pipeline yet — that is Task 014.
- Do not implement display logic yet — that is Task 013.
- The stubs added to `termProgressBar` in step 3 are intentionally temporary.

---

# Task 012 Review: Extend ProgressReporter Interface with Retry and Duration Events

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task added `Retry()` and `FetchDuration(time.Duration)` to the `ProgressReporter` interface in `quality/progress.go`, added no-op implementations to `NoopProgressReporter`, and added stub implementations to `termProgressBar` in `main.go` to keep the build green.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/progress.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `ProgressReporter` interface has `Retry()` and `FetchDuration(time.Duration)` methods | PASS |
| `NoopProgressReporter` compiles with no-op implementations | PASS |
| `termProgressBar` compiles with stub implementations | PASS |
| `go build ./...` passes with no errors | PASS |
| Existing tests pass unchanged | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Compile-time interface guard:** The `var _ ProgressReporter = NoopProgressReporter{}` assertion in `quality/progress.go` ensures the no-op implementation stays in sync with the interface at compile time.

---

## Verification Commands Run

```bash
make test   # all packages pass
make lint   # go vet clean, no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The interface, no-op, and stub implementations match the specification exactly, and the build and tests are green.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
