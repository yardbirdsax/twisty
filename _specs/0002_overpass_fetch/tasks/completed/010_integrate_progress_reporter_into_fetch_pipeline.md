---
# Task 010: Integrate ProgressReporter into FetchTiledWays

## Summary

Add a `Progress ProgressReporter` field to `TileFetchConfig` and wire it into `FetchTiledWays` so that `SetTotal`, `Tick`, and `Done` are called at the correct points in the tile loop. Default to `NoopProgressReporter` when the field is nil.

## Dependencies

Task 009 — `ProgressReporter` interface and `NoopProgressReporter` must exist.

## Detailed Directions

### 1. Add the field to `TileFetchConfig`

In `quality/tilefetch.go`, add a `Progress` field to the config struct:

```go
type TileFetchConfig struct {
    Endpoint       string
    TileSize       float64
    Cache          *TileCache
    NoCache        bool
    Logger         *slog.Logger
    RateLimitDelay time.Duration
    RetryDelay     time.Duration
    Progress       ProgressReporter // nil → NoopProgressReporter
}
```

### 2. Default nil to NoopProgressReporter at the top of FetchTiledWays

In the existing block that defaults nil/zero config fields, add:

```go
if cfg.Progress == nil {
    cfg.Progress = NoopProgressReporter{}
}
```

### 3. Call SetTotal after the tile list is computed

Immediately after `tiles := ComputeTiles(...)`, call:

```go
cfg.Progress.SetTotal(len(tiles))
```

### 4. Call Tick after each tile is processed

At the bottom of the tile loop, after the `if raw == nil { continue }` guard but before `allTileData = append(...)`, call:

```go
cfg.Progress.Tick(cached)
```

To support this, introduce a `cached bool` local variable at the top of each loop iteration:

```go
for _, tile := range tiles {
    var raw []byte
    cached := false

    if !cfg.NoCache && cfg.Cache.Has(tile) {
        // ...
        if err == nil {
            cacheHits++
            cached = true
        }
    }

    // ... fetch path unchanged ...

    if raw == nil {
        continue
    }

    cfg.Progress.Tick(cached)
    allTileData = append(allTileData, raw)
}
```

### 5. Call Done after the loop

Immediately after the loop and the summary log call, before calling `mergeAndDeduplicate`, add:

```go
cfg.Progress.Done()
```

## Acceptance Criteria

- [ ] `TileFetchConfig` has a `Progress ProgressReporter` field.
- [ ] A nil `Progress` value is replaced with `NoopProgressReporter{}` before the loop.
- [ ] `SetTotal` is called once with the correct tile count before the loop begins.
- [ ] `Tick(true)` is called for each tile served from cache.
- [ ] `Tick(false)` is called for each tile fetched from the API.
- [ ] `Tick` is NOT called for failed tiles (tiles where `raw == nil` after all retries).
- [ ] `Done` is called exactly once after the loop completes (including on the context-cancelled early-return path).
- [ ] All existing unit and integration tests continue to pass (`go test ./...`).

## Notes

- Failed tiles (where `raw` remains `nil`) should not call `Tick` — the total and current counts will diverge, which is acceptable and informative.
- The context-cancelled early-return path in the loop (after `sleepWithContext` returns an error) currently returns early via `mergeAndDeduplicate`. Ensure `Done()` is called on this path too — consider a `defer cfg.Progress.Done()` at the top of the function to guarantee it.

---

---
# Task 010 Review: Integrate ProgressReporter into FetchTiledWays

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task added a `Progress ProgressReporter` field to `TileFetchConfig` and wired `SetTotal`, `Tick`, and `Done` into `FetchTiledWays`. The `defer cfg.Progress.Done()` pattern handles all return paths including context-cancelled early exits. Three spy-based integration tests verify correct call counts and arguments.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/progress.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `TileFetchConfig` has a `Progress ProgressReporter` field | PASS |
| A nil `Progress` value is replaced with `NoopProgressReporter{}` before the loop | PASS |
| `SetTotal` is called once with the correct tile count before the loop begins | PASS |
| `Tick(true)` is called for each tile served from cache | PASS |
| `Tick(false)` is called for each tile fetched from the API | PASS |
| `Tick` is NOT called for failed tiles | PASS |
| `Done` is called exactly once after the loop completes (including context-cancelled path) | PASS |
| All existing unit and integration tests continue to pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **defer for Done:** Using `defer cfg.Progress.Done()` guarantees `Done` fires on all return paths without duplication, including the `EnsureDir` early-return path.
2. **Spy-based behavioral tests:** `spyProgressReporter` records all calls, enabling precise assertion on argument values and call counts rather than relying on code inspection alone.

---

## Verification Commands Run

```bash
make test   # PASS — all packages pass (quality: 5.058s)
make lint   # PASS — go vet reports no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, all tests pass, linter is clean, and the implementation correctly covers all code paths including context cancellation.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
