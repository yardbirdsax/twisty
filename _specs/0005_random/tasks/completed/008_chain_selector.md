---
# Task 008: ChainSelector Implementation

## Summary

Implement `ChainSelector` — a `WaypointSelector` strategy that builds a continuous chain of scored road collections forming a loop, then extracts dense waypoints along the actual road geometry. Unlike the previous selectors that drop sparse waypoints and hope Valhalla connects them well, this approach gives Valhalla a dense breadcrumb trail that forces it to follow the twisty roads.

## Dependencies

- Task 001 (waypoint package foundation — interface, helpers)
- The `quality` package's `RoadCollection` with its `Segments` slice (existing)
- `geo.Bearing`, `geo.Haversine`, `geo.AngleDiff` (existing)

## Detailed Directions

### 1. Create `waypoint/chain_selector.go`

```go
package waypoint

import (
    "math"
    "math/rand"
    "sort"

    "github.com/yardbirdsax/twisty/geo"
    "github.com/yardbirdsax/twisty/quality"
)

// ChainSelector builds a continuous chain of scored road collections
// and extracts dense waypoints along their geometry to produce a
// loop-shaped route that follows twisty roads rather than letting
// Valhalla fill gaps with boring connectors.
type ChainSelector struct {
    Start         geo.Coord  // origin point
    Radius        float64    // effective radius in km
    ArcWidth      float64    // sector width in degrees (default: 120)
    AvgSpeedMPH   float64    // for time estimation (default: DefaultAvgSpeedMPH)
    TimeBudgetSec float64    // total ride time budget in seconds
    Rand          *rand.Rand // injectable for deterministic testing
}

// Compile-time assertion.
var _ WaypointSelector = (*ChainSelector)(nil)
```

### 2. Implement Select

The `Select` method ignores the `count` parameter (it determines waypoint count from the chain length). The interface signature is unchanged for compatibility; `count` is unused.

```go
func (s *ChainSelector) Select(
    collections []quality.RoadCollection,
    count int,
    origin geo.Coord,
    radiusKm float64,
) []geo.Coord
```

**Algorithm overview:**

#### Step A: Build eligible list

Same filters as other selectors:
- `PenalizedScore > 0`
- `TotalLength >= MinRoadLengthM`
- Midpoint within `radiusKm` of `origin`

For each eligible collection, precompute and store:
- `midpoint` (via `CollectionMidpoint`)
- `bearing` from origin to midpoint
- `distanceFromStart` (haversine, meters)
- `estimatedTimeSec` = `collection.TotalLength / avgSpeedMS` (where `avgSpeedMS = avgSpeedMPH * 1609.34 / 3600`)

#### Step B: Pick a random outbound direction

Same as `SectorLobeSelector`: uniform random bearing 0–360°. Filter eligible collections to those within `±(arcWidth/2)` of this bearing. If fewer than 3 candidates, widen by 30° increments up to 360° (full circle fallback).

#### Step C: Build the chain (greedy)

This is the core of the algorithm. Maintain:
- `chain []quality.RoadCollection` — ordered list of collections to visit
- `visited map[int]bool` — indices into the eligible list already in the chain (no repeats)
- `cumulativeTimeSec float64` — estimated time consumed so far (road time + connector time)
- `currentPos geo.Coord` — the "cursor" position (end of the last collection, or start for the first pick)
- `timeBudgetSec float64` — from `s.TimeBudgetSec`

**Initial pick:** Select the nearest eligible collection to the start point (in the sector). This gets the rider to twists quickly.

**Chain loop:** While `cumulativeTimeSec < timeBudgetSec * 0.85` (reserve 15% for the ride home):

1. For each unvisited eligible collection, compute a **chain score**:

   ```
   connectorDistM  = geo.Haversine(currentPos, candidateMidpoint)
   connectorTimeSec = connectorDistM / avgSpeedMS
   totalCandidateTimeSec = connectorTimeSec + candidateEstimatedTimeSec

   // Would adding this candidate bust the budget?
   timeAfter = cumulativeTimeSec + totalCandidateTimeSec + returnTimeSec
   ```

   Where `returnTimeSec = geo.Haversine(candidateEndpoint, origin) / avgSpeedMS` — estimated time to get home from the far end of this candidate.

   If `timeAfter > timeBudgetSec`, skip this candidate (it would blow the budget).

   Otherwise:

   ```
   chainScore = candidate.PenalizedScore / (connectorTimeSec + 1)
   ```

   This favors high-scoring roads reachable with short connectors. The `+1` prevents division by zero for adjacent collections.

2. **Homeward bias:** After `cumulativeTimeSec > timeBudgetSec * 0.5` (past the halfway point), add a multiplier that favors candidates closer to home:

   ```
   homewardFactor = 1.0 + (distanceFromStart_current - distanceFromStart_candidate) / distanceFromStart_current
   chainScore *= max(homewardFactor, 0.5)
   ```

   This gently steers the chain back toward the start without hard-cutting the exploration.

3. Select the candidate with the highest `chainScore`. Add it to the chain, mark visited, update `currentPos` to the endpoint of the collection, and add the road time + connector time to `cumulativeTimeSec`.

4. If no candidates remain within budget, stop.

#### Step D: Extract dense waypoints from the chain

For each collection in the chain, extract waypoints from its actual segment geometry:

```go
// waypointInterval controls spacing between extracted waypoints.
const waypointInterval = 2000.0 // meters — one waypoint every ~2km along the road
```

For each collection:
1. Walk the `Segments` slice, accumulating distance.
2. Every `waypointInterval` meters, emit the current segment's midpoint as a waypoint.
3. Always emit the first and last points of the collection (so Valhalla enters and exits correctly).

Deduplicate any waypoints that are within 100m of each other (consecutive chains may share endpoints).

#### Step E: Cap waypoint count

Valhalla has practical limits on waypoint count. If the extracted waypoints exceed 20, uniformly subsample to 20 while always keeping the first and last points.

Return the final waypoint list.

### 3. Implement helper: `collectionEndpoints`

```go
// collectionEndpoints returns the start and end coordinates of a RoadCollection
// from its first and last segments.
func collectionEndpoints(c quality.RoadCollection) (start, end geo.Coord, ok bool) {
    if len(c.Segments) == 0 {
        return geo.Coord{}, geo.Coord{}, false
    }
    return c.Segments[0].Start, c.Segments[len(c.Segments)-1].End, true
}
```

### 4. Implement helper: `extractDenseWaypoints`

```go
// extractDenseWaypoints walks a chain of collections and emits waypoints
// at approximately intervalM-meter spacing along the actual road geometry.
func extractDenseWaypoints(chain []quality.RoadCollection, intervalM float64) []geo.Coord
```

Logic:
- For each collection, iterate segments and accumulate `seg.Length`.
- When accumulated distance >= `intervalM`, emit the segment endpoint and reset accumulator.
- Always emit the first point of the first collection and last point of the last collection.
- Deduplicate consecutive points within 100m.

### 5. Implement helper: `subsampleWaypoints`

```go
// subsampleWaypoints reduces a waypoint slice to at most maxN points,
// preserving the first and last points and spacing the rest uniformly.
func subsampleWaypoints(coords []geo.Coord, maxN int) []geo.Coord
```

### 6. Write unit tests

`waypoint/chain_selector_test.go`:

- **Greedy chain builds in order:** Given 5 collections at known positions, verify the chain visits them in an order consistent with nearest-good-road greedy logic (not alphabetical, not random).
- **No repeats:** A collection cannot appear twice in the chain.
- **Time budget respected:** With a short time budget, the chain stops before visiting all candidates; `returnTimeSec` is always accounted for.
- **Homeward bias:** Past the halfway point, candidates closer to home are preferred over equidistant ones further away.
- **Budget blow protection:** A candidate whose addition would exceed the budget (including estimated return time) is skipped even if it has the highest score.
- **Dense waypoints:** `extractDenseWaypoints` with a 1000m interval on a 5000m collection produces ~5 waypoints plus endpoints.
- **Subsample:** 30 waypoints subsampled to 20 preserves first and last, spaces evenly.
- **Sector filtering:** Only collections in the outbound sector are chained.
- **Deterministic with fixed seed.**
- **Eligibility filters:** Same as other selectors (score > 0, length >= min, within radius).

### 7. Confirm interface compliance

`var _ WaypointSelector = (*ChainSelector)(nil)` must compile.

## Acceptance Criteria

- [ ] `ChainSelector` implements `WaypointSelector` (compile-time assertion passes).
- [ ] Chain is built greedily, favoring high score-to-connector-cost ratio.
- [ ] No road collection appears twice in the chain (test verified).
- [ ] Time budget is respected: chain stops when adding the next candidate + return would exceed budget (test verified).
- [ ] Homeward bias activates past 50% of time budget (test verified).
- [ ] Dense waypoints are extracted at ~`waypointInterval` spacing from actual segment geometry (test verified).
- [ ] Waypoint count is capped at 20 (test verified).
- [ ] Sector filtering limits candidates to the outbound direction (test verified).
- [ ] With a fixed seed, output is deterministic (test verified).
- [ ] `go test ./waypoint/...` passes.

## Notes

- The `count` parameter from the interface is ignored. The `ChainSelector` determines waypoint count from chain length and density. This is acceptable per the interface contract — `count` is a hint, not a hard requirement.
- The `waypointInterval` (2000m) and max waypoint cap (20) are package-level constants. They may need tuning after field testing.
- The `0.85` budget reserve and `0.5` homeward-bias trigger are tuning knobs. Start with these values and adjust based on testing.
- The greedy approach is intentionally simple. It does not attempt to find the optimal chain — it builds a good-enough one in O(n²) time (n = eligible collections, typically < 100).
- `SectorLobeSelector` and `WeightedRandomSelector` are not modified or deleted by this task.

---

---
# Task 008 Review: ChainSelector Implementation

**Reviewer:** Senior Software Engineer Agent
**Date:** 2026-03-21
**Verdict:** APPROVED

---

## Summary

This task implements `ChainSelector`, a `WaypointSelector` that builds a greedy chain of scored road collections into a loop and extracts dense waypoints along actual segment geometry. The algorithm, interface compliance, constants, helpers, and tests are all present and correct.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/chain_selector.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/chain_selector_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `ChainSelector` implements `WaypointSelector` (compile-time assertion passes) | PASS |
| Chain is built greedily, favoring high score-to-connector-cost ratio | PASS |
| No road collection appears twice in the chain (test verified) | PASS |
| Time budget is respected: chain stops when adding next candidate + return would exceed budget (test verified) | PASS |
| Homeward bias activates past 50% of time budget (test verified) | PASS |
| Dense waypoints are extracted at ~`waypointInterval` spacing from actual segment geometry (test verified) | PASS |
| Waypoint count is capped at 20 (test verified) | PASS |
| Sector filtering limits candidates to the outbound direction (test verified) | PASS |
| With a fixed seed, output is deterministic (test verified) | PASS |
| `go test ./waypoint/...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test ./waypoint/... -v  # all 15 ChainSelector tests PASS; TimeBudgetRespected: short=4 waypoints, long=14 waypoints
go vet ./waypoint/...       # clean
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass. Tests are correctly enforced, the budget comparison produces a meaningful differential (4 vs 14 waypoints), and `go vet` is clean.
