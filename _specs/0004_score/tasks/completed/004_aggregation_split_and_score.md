# Task 004: Aggregation — Split at Straight Gaps and Compute Scores

## Summary

Implement the second half of stage 5: splitting ordered way chains at long straight gaps and computing aggregate scores for each resulting road collection. This completes the aggregation pipeline.

## Dependencies

Task 003 — requires grouping, connected components, and ordering functions.

## Detailed Directions

### 1. Define Split Threshold Constant

- In `quality/scoring_params.go`, add:

```go
const StraightGapSplitM = 2414.0 // 1.5 miles in meters — matches Curvature project
```

### 2. Implement Straight-Gap Splitting

- In `quality/aggregate.go`, add:

```go
// SplitAtStraightGaps splits an ordered slice of ways at contiguous runs
// of zero-score (tier 0) segments exceeding the threshold distance.
// Returns one or more sub-slices of segments, each representing a
// contiguous section of the road.
func SplitAtStraightGaps(ways []ScoredWay, thresholdM float64) [][]ScoredSegment
```

- Walk through all segments across all ways in order. Track consecutive tier-0 segment length.
- When the accumulated tier-0 length exceeds `thresholdM`, split: everything before the straight run becomes one group, and a new group starts after.
- Include the straight segments in whichever group they're adjacent to (or split them between groups) — the simplest approach is to end the current group before the straight run and start the new group after it.

### 3. Implement Collection Builder

- Add a function that takes the outputs of grouping, connecting, ordering, and splitting to produce `[]RoadCollection`:

```go
// Aggregate processes all scored ways into road collections.
// This is the main entry point for stage 5.
func Aggregate(ways []ScoredWay) []RoadCollection
```

- Flow:
  1. `GroupWaysByName(ways)` → name groups
  2. For each name group: `FindConnectedComponents(ways, ConnectedEndpointProximityM)` → components
  3. For each component: `OrderWays(ways)` → ordered chain
  4. For each ordered chain: `SplitAtStraightGaps(orderedWays, StraightGapSplitM)` → segment groups
  5. For each segment group: build a `RoadCollection` with computed scores

### 4. Implement Score Computation

- For each `RoadCollection`, compute:
  - `TotalScore`: sum of all segment `Score` values
  - `TotalLength`: sum of all segment `Length` values
  - `ScorePerKm`: `TotalScore / (TotalLength / 1000.0)` (guard against zero length)
  - `HighwayTypes`: deduplicated list of `highway` tag values from the constituent ways
  - `WayIDs`: list of way IDs from constituent ways

### 5. Handle Sub-Indexing

- When a named road produces multiple collections (from connectivity splitting or straight-gap splitting), assign incrementing `SubIndex` values (0, 1, 2, ...).
- A named road that produces only one collection gets `SubIndex = 0`.

### 6. Write Unit Tests

- `TestSplitAtStraightGaps`: chain with a long straight section in the middle produces two groups; chain with short straights stays as one group
- `TestAggregate`: end-to-end test with synthetic ways that exercises grouping, connectivity, ordering, splitting, and scoring
- `TestAggregateScoreComputation`: verify total score, total length, and score per km calculations
- `TestAggregateSubIndexing`: named road split into multiple collections has correct sub-indices
- Edge cases: all segments tier-0 (single collection with score 0), single-way road, empty input

## Acceptance Criteria

- [ ] `SplitAtStraightGaps` correctly splits at long straight runs and preserves segments
- [ ] `Aggregate` produces correctly grouped, connected, ordered, split, and scored collections
- [ ] `TotalScore`, `TotalLength`, and `ScorePerKm` are computed accurately
- [ ] Sub-indexing works correctly for split roads
- [ ] `HighwayTypes` and `WayIDs` are populated from constituent ways
- [ ] All unit tests pass

## Notes

- The `Aggregate` function is the single entry point for stage 5. It should be straightforward to call from the pipeline orchestration.
- Collections with `TotalScore == 0` are retained — filtering happens at KML output time via `-min-score`.
- The split threshold of 2,414 meters (1.5 miles) matches the Curvature project.

---
# Task 004 Review: Aggregation — Split at Straight Gaps and Compute Scores

**Reviewer:** Claude Opus 4.6
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task implements `SplitAtStraightGaps`, `Aggregate`, and `buildRoadCollection` to complete the stage 5 aggregation pipeline. All blocking issues from the prior review cycle have been resolved: `WayID` was added to `ScoredSegment`, `buildRoadCollection` now derives `WayIDs`/`HighwayTypes` per split group, `StraightGapSplitM` is included in `ScoringParamsHash`, and `TestAggregate_TwoDisconnectedClusters` was added.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/aggregate.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/aggregate_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/curvature.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `SplitAtStraightGaps` correctly splits at long straight runs and preserves segments | PASS |
| `Aggregate` produces correctly grouped, connected, ordered, split, and scored collections | PASS |
| `TotalScore`, `TotalLength`, and `ScorePerKm` are computed accurately | PASS |
| Sub-indexing works correctly for split roads | PASS |
| `HighwayTypes` and `WayIDs` are populated from constituent ways | PASS |
| All unit tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test   # all packages pass
make lint   # go vet clean, no issues reported
go test ./quality/... -v -run "TestSplitAtStraightGaps|TestAggregate|TestAggregateScore|TestAggregateSubIndexing"  # all 11 targeted tests pass
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The two blocking issues from the prior review (incorrect `WayIDs`/`HighwayTypes` scoping and missing `StraightGapSplitM` in the params hash) are resolved. The connectivity-driven split test is present and passing.
