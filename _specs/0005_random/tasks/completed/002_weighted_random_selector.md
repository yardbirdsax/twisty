---
# Task 002: WeightedRandomSelector Implementation

## Summary

Implement the `WeightedRandomSelector` — the default `WaypointSelector` strategy — in the `waypoint` package. It samples road collections without replacement, weighted by `PenalizedScore`, extracts midpoint coordinates, and sorts them by clockwise bearing from the start point.

## Dependencies

Task 001 — the `waypoint` package with the interface and helpers must exist.

## Detailed Directions

### 1. Add the selector to `waypoint/selector.go`

Create a new file `waypoint/selector.go` (keep it separate from the interface file for clarity):

```go
package waypoint

import (
    "math/rand"

    "github.com/yardbirdsax/twisty/geo"
    "github.com/yardbirdsax/twisty/quality"
)

const MinRoadLengthM = 500.0 // collections shorter than this are excluded

// WeightedRandomSelector samples road collections without replacement,
// with probability proportional to PenalizedScore.
type WeightedRandomSelector struct {
    Rand *rand.Rand // injectable for deterministic testing; nil uses global rand
}
```

### 2. Implement Select

```go
func (s *WeightedRandomSelector) Select(
    collections []quality.RoadCollection,
    count int,
    origin geo.Coord,
    radiusKm float64,
) []geo.Coord
```

**Note:** The interface defined in Task 001 does not include `origin` or `radiusKm` — the radius pre-filter and bearing sort are the caller's responsibility for the basic interface. However, the `WeightedRandomSelector` needs `origin` for bearing sort. Extend the `WaypointSelector` interface signature in `waypoint.go` to:

```go
type WaypointSelector interface {
    Select(collections []quality.RoadCollection, count int, origin geo.Coord, radiusKm float64) []geo.Coord
}
```

This signature carries all context the selector needs; the radius filter ensures contraction retries (Task 005) need no changes to the selector itself.

**Algorithm:**

1. Build an eligible list: collections where `PenalizedScore > 0` AND `TotalLength >= MinRoadLengthM` AND the midpoint falls within `radiusKm` of `origin`.
2. If the eligible list is smaller than `count`, use all eligible collections.
3. Perform weighted sampling without replacement using `PenalizedScore` as weight:
   - Compute cumulative weights.
   - On each draw, pick a random value in `[0, totalWeight)`, binary-search to find the selected index, then zero out its weight (so it cannot be drawn again) and add it to the result.
4. For each selected collection, call `CollectionMidpoint` to get the coordinate.
5. Call `SortByBearing(origin, coords)`.
6. Return the sorted coords.

### 3. Implement weighted sampling helper

A small unexported helper keeps Select readable:

```go
// weightedSampleWithoutReplacement draws n indices from weights without replacement.
// Weights are modified in place (zeroed on draw).
func weightedSampleWithoutReplacement(rng *rand.Rand, weights []float64, n int) []int
```

Use the rng parameter if non-nil, otherwise `rand.Float64()`.

### 4. Write unit tests

`waypoint/selector_test.go`:

- With a fixed seed `rand.New(rand.NewSource(42))`, selecting 3 from 10 collections returns deterministic results.
- Collections with `PenalizedScore == 0` are never selected.
- Collections with `TotalLength < MinRoadLengthM` are never selected.
- Collections whose midpoints fall outside `radiusKm` are never selected.
- When fewer eligible collections exist than `count`, all eligible collections are returned.
- Returned coords are sorted in ascending bearing order from the origin.

### 5. Confirm interface compliance

Add a compile-time assertion:

```go
var _ WaypointSelector = (*WeightedRandomSelector)(nil)
```

## Acceptance Criteria

- [ ] `WeightedRandomSelector` implements `WaypointSelector` (compile-time assertion passes).
- [ ] Collections with `PenalizedScore == 0` or `TotalLength < MinRoadLengthM` are never selected (test verified).
- [ ] Collections outside `radiusKm` from origin are never selected (test verified).
- [ ] With a fixed random seed, output is deterministic (test verified).
- [ ] Result coords are sorted by bearing from origin.
- [ ] `go test ./waypoint/...` passes.

## Notes

- The `Rand` field being nil-safe (falling back to the global `rand` functions) keeps production use simple (`&WeightedRandomSelector{}`) while test code injects a seeded source.
- `MinRoadLengthM = 500.0` is a package-level constant. Adjust if field testing shows it's too restrictive.

---

---
# Task 002 Review: WeightedRandomSelector Implementation

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-21
**Verdict:** APPROVED

---

## Summary

Implements `WeightedRandomSelector` in `waypoint/selector.go` — a weighted-without-replacement sampler that filters by score, length, and radius, then returns midpoints sorted by bearing from origin. The `WaypointSelector` interface in `waypoint.go` was updated to carry `origin` and `radiusKm`.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/selector.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/selector_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/waypoint.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `WeightedRandomSelector` implements `WaypointSelector` (compile-time assertion) | PASS |
| Collections with `PenalizedScore == 0` or `TotalLength < MinRoadLengthM` never selected | PASS |
| Collections outside `radiusKm` from origin never selected | PASS |
| Fixed random seed produces deterministic output | PASS |
| Result coords sorted by bearing from origin | PASS |
| `go test ./waypoint/...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## Good Practices Observed

1. **Nil-safe RNG injection:** The `Rand` field falls back to `rand.Float64` when nil, keeping production use simple while enabling deterministic testing.
2. **Floating-point fallback in sampling:** The fallback loop after `chosen == -1` handles edge cases where `r` rounds up to `totalWeight`.
3. **Eligible struct avoids double iteration:** Midpoint is computed once during eligibility check and reused at extraction.

---

## Verification Commands Run

```bash
go test -v -short ./waypoint/...  # all 6 selector tests + existing tests PASS
go vet ./waypoint/...              # no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, tests pass, and `go vet` reports no issues.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---
