---
# Task 006: SectorLobeSelector Implementation

## Summary

Implement `SectorLobeSelector` — a new `WaypointSelector` strategy in the `waypoint` package that concentrates waypoints in an angular sector from the start point and orders them to form a lobe-shaped route: nearest twisty road first on the outbound leg, farthest at the apex, gradual return. This replaces `WeightedRandomSelector` as the default but does not delete it.

## Dependencies

- Task 001 (waypoint package foundation)
- Task 002 (WeightedRandomSelector — retained but no longer default)

## Detailed Directions

### 1. Add the selector to `waypoint/sector_selector.go`

Create a new file `waypoint/sector_selector.go`:

```go
package waypoint

import (
    "math/rand"

    "github.com/yardbirdsax/twisty/geo"
    "github.com/yardbirdsax/twisty/quality"
)

// DefaultArcWidth is the default angular width of the sector in degrees.
const DefaultArcWidth = 90.0

// arcWideningStep is the amount to widen the arc when too few candidates exist.
const arcWideningStep = 30.0

// maxArcWidth is the widest the sector can grow before falling back to full circle.
const maxArcWidth = 180.0

// SectorLobeSelector picks a random outbound direction and concentrates waypoints
// in an angular sector to produce a lobe-shaped route that gets to twisty roads
// quickly and works its way back gradually.
type SectorLobeSelector struct {
    Start    geo.Coord  // origin for bearing/distance calculations
    Radius   float64    // effective radius in km (for contraction filtering)
    ArcWidth float64    // sector width in degrees (default: 90)
    Rand     *rand.Rand // injectable for deterministic testing; nil uses global rand
}

// Compile-time assertion.
var _ WaypointSelector = (*SectorLobeSelector)(nil)
```

### 2. Implement Select

```go
func (s *SectorLobeSelector) Select(
    collections []quality.RoadCollection,
    count int,
    origin geo.Coord,
    radiusKm float64,
) []geo.Coord
```

**Algorithm:**

1. **Pick a random outbound bearing** (0–360°, uniform). Use `s.Rand` if non-nil, else `rand.Float64`.

2. **Build the eligible list** (same filters as `WeightedRandomSelector`):
   - `PenalizedScore > 0`
   - `TotalLength >= MinRoadLengthM`
   - Midpoint within `radiusKm` of `origin` (haversine, meters)

   For each eligible collection, compute and store:
   - `midpoint` (via `CollectionMidpoint`)
   - `bearing` from origin to midpoint (via `geo.Bearing`)
   - `distance` from origin to midpoint (via `geo.Haversine`)

3. **Filter to sector:** Keep only collections whose bearing falls within `±(arcWidth/2)` of the outbound bearing. Use `geo.AngleDiff` for wraparound-safe comparison:
   ```go
   geo.AngleDiff(bearing, outboundBearing) <= arcWidth/2
   ```

4. **Widen if needed:** If fewer than `count` candidates exist in the sector, widen by `arcWideningStep` (30°) and re-filter. Repeat up to `maxArcWidth` (180°). If still fewer than `count`, use all eligible collections (full circle fallback).

5. **Score-weighted sample** `count` collections from the sector candidates using the existing `weightedSampleWithoutReplacement` function.

6. **Order waypoints for lobe shape:**
   - Partition the selected waypoints into two halves of the sector arc:
     - "left" side: bearing < outbound bearing (with wraparound)
     - "right" side: bearing ≥ outbound bearing (with wraparound)
   - Sort the outbound side (pick whichever side has more waypoints, or left by default) by distance from start ascending (nearest first).
   - Sort the return side by distance from start descending (farthest first).
   - Concatenate: outbound side → return side.
   - This produces a lobe: start → nearest twist → further twists → apex → far return twist → closer return twist → start.

   If all waypoints fall on one side (narrow cluster), just sort nearest → farthest. Valhalla will route out and back naturally.

### 3. Export `weightedSampleWithoutReplacement`

The existing function in `selector.go` is unexported. Either:
- Rename it to `WeightedSampleWithoutReplacement` (exported), or
- Move it to a shared file (e.g., `waypoint/sampling.go`) and export it.

Both selectors use the same sampling logic. Avoid duplication.

### 4. Add a helper for sector membership

```go
// inSector reports whether bearing is within ±halfArc degrees of center,
// handling 360°/0° wraparound.
func inSector(bearing, center, halfArc float64) bool {
    return geo.AngleDiff(bearing, center) <= halfArc
}
```

### 5. Write unit tests

`waypoint/sector_selector_test.go`:

- **Deterministic output:** With a fixed seed, selecting 3 from 10 collections returns deterministic results.
- **Sector filtering:** Collections outside the sector arc are not selected (place 5 collections at known bearings, set a narrow arc, verify only in-sector ones are picked).
- **Arc widening:** When the sector has 0 candidates but the full circle has enough, the arc widens until candidates are found.
- **Full circle fallback:** When even 180° is insufficient, all eligible collections are used.
- **Lobe ordering:** Verify that waypoints are ordered nearest→farthest on the outbound side, farthest→nearest on the return side. Use collections at known bearings and distances.
- **Eligibility filters:** Collections with `PenalizedScore == 0`, `TotalLength < MinRoadLengthM`, or midpoint outside `radiusKm` are never selected.
- **Single-side cluster:** When all waypoints fall on one side of the arc, ordering is simply nearest→farthest.

### 6. Confirm interface compliance

The compile-time assertion `var _ WaypointSelector = (*SectorLobeSelector)(nil)` must compile.

## Acceptance Criteria

- [x] `SectorLobeSelector` implements `WaypointSelector` (compile-time assertion passes).
- [x] Waypoints are drawn only from the angular sector (test verified with known bearings).
- [x] Arc widening triggers when the sector has too few candidates (test verified).
- [x] Lobe ordering is correct: outbound nearest→farthest, return farthest→nearest (test verified).
- [x] Eligibility filters match `WeightedRandomSelector` (score > 0, length >= min, within radius).
- [x] With a fixed random seed, output is deterministic (test verified).
- [x] `go test ./waypoint/...` passes.

<!-- Fixed (2026-03-21):
  - Exported weightedSampleWithoutReplacement → WeightedSampleWithoutReplacement; updated call sites in selector.go and sector_selector.go.
  - TestSectorLobeSelector_LobeOrdering now asserts outbound coords occupy the first N positions and return coords occupy the remaining positions in the returned slice.
-->

## Notes

- The `ArcWidth` field defaults to `DefaultArcWidth` (90°) if zero. Handle this in `Select` with a `if s.ArcWidth == 0 { arcWidth = DefaultArcWidth }` guard.
- The outbound bearing is not exposed as a flag or field. It is random per call. The future `-seed` flag (PRD Future Considerations) would make it reproducible.
- `WeightedRandomSelector` is not modified or deleted by this task.

---

---
# Task 006 Review: SectorLobeSelector Implementation

**Reviewer:** Senior Software Engineer
**Date:** 2026-03-21
**Verdict:** APPROVED

---

## Summary

This task implemented `SectorLobeSelector`, a new `WaypointSelector` strategy that concentrates waypoints in a random angular sector from the start point and orders them into a lobe shape (nearest-first outbound, farthest-first return). Both previously identified blocking issues have been resolved.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/sector_selector.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/sector_selector_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/selector.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `SectorLobeSelector` implements `WaypointSelector` (compile-time assertion passes) | PASS |
| Waypoints are drawn only from the angular sector (test verified with known bearings) | PASS |
| Arc widening triggers when the sector has too few candidates (test verified) | PASS |
| Lobe ordering is correct: outbound nearest→farthest, return farthest→nearest (test verified) | PASS |
| Eligibility filters match `WeightedRandomSelector` (score > 0, length >= min, within radius) | PASS |
| With a fixed random seed, output is deterministic (test verified) | PASS |
| `go test ./waypoint/...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test ./waypoint/... -v  # all 18 tests PASS
go vet ./waypoint/...      # no issues
make test                  # waypoint PASS; other failures are pre-existing (quality/hardfilter, diagtmp network)
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. `WeightedSampleWithoutReplacement` is exported and used by both selectors. `TestSectorLobeSelector_LobeOrdering` now verifies that outbound coords occupy the first N positions and return coords occupy the remaining positions in the returned slice.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---
