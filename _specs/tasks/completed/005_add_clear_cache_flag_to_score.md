---
# Task 005: Add `--clear-cache` Flag to `score` Command

## Summary

Add a `--clear-cache` flag to the `score` command that clears both the tile cache and the score cache before running. This mirrors the `--clear-cache` flag on the `fetch` command but extends it to cover both cache types since `score` owns both.

## Dependencies

None — this is a self-contained change to `main.go` and its tests.

## Detailed Directions

### 1. Add `clearCache` field to `execScoreParams`

In `main.go`, add a `clearCache bool` field to the `execScoreParams` struct alongside the existing `clearScoreCache` field:

```go
type execScoreParams struct {
    // ... existing fields ...
    clearCache      bool   // NEW: clears tile cache AND score cache
    clearScoreCache bool
    // ...
}
```

### 2. Handle `--clear-cache` in `execScore`

After the `tileCache` and `scoreCache` are constructed (and `scoreCache.EnsureDir()` succeeds), add a block that runs when `p.clearCache` is true. It should clear both caches and print a confirmation line to stderr for each, matching the style of the existing `clearScoreCache` block:

```go
if p.clearCache {
    logger.Info("clearing tile cache", "dir", cacheDir)
    if err := tileCache.ClearAll(); err != nil {
        return fmt.Errorf("clearing tile cache: %w", err)
    }
    fmt.Fprintln(stderr, "Tile cache cleared.")

    logger.Info("clearing score cache", "dir", scoreCacheDir)
    if err := scoreCache.ClearAll(); err != nil {
        return fmt.Errorf("clearing score cache: %w", err)
    }
    fmt.Fprintln(stderr, "Score cache cleared.")
}
```

This block should run **before** the existing `clearScoreCache` block so the two flags remain independently usable. If both flags are passed, the score cache is cleared twice (harmless), but that edge case does not need special handling.

### 3. Wire the flag in `newScoreCmd`

In `newScoreCmd`, declare a `clearCache bool` local variable and register the flag:

```go
var (
    // ... existing vars ...
    clearCache      bool
    // ...
)
```

Pass it into `execScoreParams`:

```go
return execScore(execScoreParams{
    // ... existing fields ...
    clearCache:      clearCache,
    // ...
})
```

Register the flag on `f`:

```go
f.BoolVar(&clearCache, "clear-cache", false, "Delete all cached tiles and score cache entries before running")
```

### 4. Write a test

Add a test in `main_test.go` (or the existing score test file, wherever `execScore` is currently tested) that:

- Creates a temporary directory with fake tile and score cache files.
- Calls `execScore` with `clearCache: true`.
- Asserts that both cache directories are empty after the call returns (the files were deleted).
- Uses a stub/fake Overpass endpoint (or the existing test helper pattern) so the test does not make real network calls.

Follow the red-green TDD approach: write the failing test first, then make it pass.

## Acceptance Criteria

- [ ] `twisty score --clear-cache ...` clears both `~/.twisty/cache/overpass/` and `~/.twisty/cache/scores/` before fetching or scoring.
- [ ] Stderr prints `"Tile cache cleared."` and `"Score cache cleared."` when `--clear-cache` is used.
- [ ] `--clear-score-cache` continues to work independently (score cache only).
- [ ] Both flags can be passed together without error.
- [ ] A unit/integration test covers the `--clear-cache` behavior.
- [ ] `go test ./...` passes.

## Notes

- `TileCache.ClearAll()` already exists and is used by `execFetch`; reuse it directly.
- `ScoreCache.ClearAll()` already exists and is used by the existing `clearScoreCache` path in `execScore`.
- The flag name `--clear-cache` matches the `fetch` command exactly, which is intentional for consistency.
- Do not remove or rename `--clear-score-cache`; it stays for users who only want to invalidate scores.

---

# Task 005 Review: Add `--clear-cache` Flag to `score` Command

**Reviewer:** Principal Engineer
**Date:** 2026-05-05
**Verdict:** APPROVED

---

## Summary

Adds a `--clear-cache` flag to the `score` command that clears both the tile cache and score cache before scoring. Implementation follows the spec: field added to `execScoreParams`, clearing logic placed before the existing `clearScoreCache` block, flag wired in `newScoreCmd`, and a test covers the behavior.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `twisty score --clear-cache` clears both tile and score cache | PASS |
| Stderr prints "Tile cache cleared." and "Score cache cleared." | PASS |
| `--clear-score-cache` continues to work independently | PASS (code correct, no dedicated test) |
| Both flags can be passed together without error | PASS (code correct, no dedicated test) |
| A unit/integration test covers the `--clear-cache` behavior | PASS |
| `go test ./...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## Good Practices Observed

(Omitted per review instructions.)

---

## Verification Commands Run

```bash
cd /Users/joshuafeierman/repos/yardbirdsax/twisty && make test  # all relevant tests pass
cd /Users/joshuafeierman/repos/yardbirdsax/twisty && make lint  # clean
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The implementation matches the spec, the new test verifies both caches are cleared and confirmation messages printed to stderr. `--clear-score-cache` remains independently functional.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.
