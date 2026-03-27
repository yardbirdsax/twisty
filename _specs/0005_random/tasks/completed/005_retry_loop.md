---
# Task 005: Time Budget Enforcement with Incremental Retry Loop

## Summary

Replace the single-attempt pipeline in `runRandom` with a retry loop that adjusts the search radius when the route duration falls outside the tolerance window. Expansion fetches only the new outer annular ring of tiles. Contraction skips all tile work and only tightens the radius bound passed to the selector. Oscillation detection stops the loop early and returns the best result seen.

## Dependencies

- Task 004 (`runRandom` basic pipeline must exist)

## Detailed Directions

### 1. Extract the per-attempt logic into a helper

Before adding the loop, refactor the core of `runRandom` into a function that performs one attempt and returns the route:

```go
// randomAttempt runs waypoint selection and FetchLoopRoute for a single attempt.
// collections is the full scored set; effectiveRadius constrains the selector.
func randomAttempt(
    collections []quality.RoadCollection,
    nWaypoints int,
    start geo.Coord,
    effectiveRadius float64,
    selector waypoint.WaypointSelector,
) (route.Route, error)
```

This keeps the retry loop readable.

### 2. Define retry state

In `runRandom`, after the initial fetch+score pipeline completes, initialize:

```go
type attemptResult struct {
    route    route.Route
    radius   float64
}

var (
    currentRadius    = radiusKm
    scoredCollections = collections  // initially all collections at base radius
    fetchedTiles      = quality.ComputeTiles(startCoord.Lat, startCoord.Lon, radiusKm, 0.1)
    bestResult       *attemptResult
    lastDirection    string // "expand" | "contract" | ""
)
```

### 3. Implement the retry loop

```go
for attempt := 1; attempt <= *maxAttempts; attempt++ {
    r, err := randomAttempt(scoredCollections, *nWaypoints, startCoord, currentRadius, selector)
    if err != nil {
        log.Fatalf("attempt %d: %v", attempt, err)
    }

    // Track best result.
    if bestResult == nil || math.Abs(r.Duration-targetSec) < math.Abs(bestResult.route.Duration-targetSec) {
        bestResult = &attemptResult{route: r, radius: currentRadius}
    }

    // Check tolerance window.
    if r.Duration >= minSec && r.Duration <= maxSec {
        bestResult = &attemptResult{route: r, radius: currentRadius}
        break
    }

    if attempt == *maxAttempts {
        fmt.Fprintf(os.Stderr,
            "Warning: max attempts (%d) reached; using closest result (actual %s)\n",
            *maxAttempts, formatDuration(bestResult.route.Duration))
        break
    }

    // Determine adjustment direction.
    var direction string
    if r.Duration < minSec {
        direction = "expand"
    } else {
        direction = "contract"
    }

    // Oscillation detection.
    if lastDirection != "" && direction != lastDirection {
        fmt.Fprintf(os.Stderr, "Oscillation detected; accepting best result (%s)\n",
            formatDuration(bestResult.route.Duration))
        break
    }
    lastDirection = direction

    if direction == "expand" {
        newRadius := currentRadius * (1 + *radiusStep)
        fmt.Fprintf(os.Stderr,
            "Attempt %d: route too short (%s), expanding radius to %.1fkm…\n",
            attempt+1, formatDuration(r.Duration), newRadius)

        // Fetch only the new outer annular ring.
        newTiles := quality.ComputeTiles(startCoord.Lat, startCoord.Lon, newRadius, 0.1)
        outerTiles := tileSetDifference(newTiles, fetchedTiles)

        if len(outerTiles) > 0 {
            outerWays, err := quality.FetchTiledWaysForTiles(ctx, outerTiles, cfg)
            if err != nil {
                log.Fatalf("expand fetch: %v", err)
            }
            outerResult := quality.RunScorePipeline(outerWays)
            outerCollections := quality.Aggregate(outerResult.ScoredWays)
            quality.ApplyPenalties(outerCollections)
            scoredCollections = append(scoredCollections, outerCollections...)
        }

        fetchedTiles = newTiles
        currentRadius = newRadius

    } else { // contract
        newRadius := currentRadius * (1 - *radiusStep)
        fmt.Fprintf(os.Stderr,
            "Attempt %d: route too long (%s), contracting radius to %.1fkm…\n",
            attempt+1, formatDuration(r.Duration), newRadius)
        // No tile work needed. Just tighten the effective radius.
        currentRadius = newRadius
    }
}
```

### 4. Implement tileSetDifference

Add a small unexported helper that computes the set of tiles present in `expanded` but not in `existing`. Use a string key derived from the tile bounds (to match how the cache keys tiles):

```go
func tileSetDifference(expanded, existing []quality.Tile) []quality.Tile {
    seen := make(map[string]bool, len(existing))
    for _, t := range existing {
        seen[tileKey(t)] = true
    }
    var result []quality.Tile
    for _, t := range expanded {
        if !seen[tileKey(t)] {
            result = append(result, t)
        }
    }
    return result
}

func tileKey(t quality.Tile) string {
    return fmt.Sprintf("%.6f,%.6f", t.South, t.West)
}
```

### 5. Check whether FetchTiledWaysForTiles exists

`quality.FetchTiledWays` takes a center+radius and internally computes its own tile set. For the expansion step we need to fetch only specific tiles. Check `quality/tilefetch.go`:

- If `FetchTiledWays` accepts a tile slice directly (or has an inner function that does), use it.
- If not, add an exported `FetchTiledWaysForTiles(ctx, tiles []Tile, cfg TileFetchConfig) ([]Way, error)` to `quality/tilefetch.go` that runs the same per-tile fetch loop but over the provided slice instead of computing its own.

This is the only change permitted to the `quality` package in this task.

### 6. Use bestResult for output

After the loop exits, replace the single-attempt route variable with `bestResult.route` for the GPX write and summary print.

### 7. Write unit tests for tileSetDifference

In `main_test.go` (or a new `random_test.go` if preferred):

- Two identical tile sets → empty difference.
- Expanded set has 4 tiles, existing has 2 of them → difference returns the 2 new ones.

## Acceptance Criteria

- [ ] A route that comes back too short triggers expansion, fetches only the outer ring, and retries.
- [ ] A route that comes back too long triggers contraction with no tile fetch.
- [ ] Oscillation (expand then contract, or vice versa) stops the loop immediately.
- [ ] Max-attempts exhaustion stops the loop and prints the warning.
- [ ] The route accepted after retry is the one closest to the target duration.
- [ ] Retry announcements are printed to stderr in the format specified in the PRD.
- [ ] `tileSetDifference` unit tests pass.
- [ ] `go test ./...` passes.

## Notes

- If `FetchTiledWaysForTiles` doesn't exist, add it to `quality/tilefetch.go` with minimal changes — just expose the inner loop over a caller-supplied tile list.
- The tile key format (`"%.6f,%.6f"`) must match whatever format the cache uses internally, or the set difference may produce redundant fetches. Check `TileCache` key generation in `tilefetch.go` before finalizing.
- `radiusStep` is validated to be > 0 in the flag setup (Task 004). No additional validation needed here.

---

---
# Task 005 Review: Time Budget Enforcement with Incremental Retry Loop

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-21
**Verdict:** APPROVED

---

## Summary

Implements a retry loop in `runRandom` that adjusts the search radius when the route duration falls outside the tolerance window. Expansion fetches only the new outer annular ring of tiles via the new `FetchTiledWaysForTiles` function. Contraction skips tile work and tightens the effective radius. Oscillation detection stops the loop early. The best result (closest to target duration) is used for GPX output.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/main_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/pipeline_integration_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| Too-short route triggers expansion, fetches outer ring, retries | PASS |
| Too-long route triggers contraction with no tile fetch | PASS |
| Oscillation stops loop immediately | PASS |
| Max-attempts exhaustion stops loop and prints warning | PASS |
| Best result (closest to target) is used for output | PASS |
| Retry announcements printed to stderr in PRD-specified format | PASS |
| `tileSetDifference` unit tests pass | PASS |
| `go test ./...` passes (excluding pre-existing failures unrelated to this task) | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test -short -count=1 .                           # PASS — main package including TestTileSetDifference
go vet ./...                                        # PASS — no issues
go test -short -count=1 ./...                       # quality/diag failures are pre-existing (confirmed below)
git stash && go test -short ./quality/... ./diag/... && git stash pop  # same failures exist before this task's changes
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The retry loop is correctly implemented: expansion triggers `FetchTiledWaysForTiles` with only the outer annular ring; contraction updates only the radius; oscillation detection and max-attempt exhaustion both exit early and emit the correct stderr messages using the `…` character. The `tileSetDifference` unit tests cover both the identical-sets and partial-overlap cases. Failing tests in `quality` and `diag` packages are pre-existing and pre-date this branch.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---
