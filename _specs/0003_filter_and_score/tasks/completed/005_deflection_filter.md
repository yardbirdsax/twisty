# Task 005: Deflection Filter

## Summary

Implement the deflection filter that zeroes out curvature scores for segments that are minor jogs in an otherwise straight road — intersection doglegs, GPS noise, and property-line wiggles. This is stage 4 of the pipeline.

## Dependencies

- Task 001 (scoring parameters — `DeflectionLookAheadM`, `DeflectionMinHeadingChange`)
- Task 004 (segment scoring — `ScoredSegment`, `ScoredWay`)

## Detailed Directions

### 1. Create `quality/deflection.go`

- Create the file `quality/deflection.go` with package `quality`.
- Implement the function:

```go
// DeflectionFilter zeroes out curvature scores for segments that are minor
// heading deviations in an otherwise straight road. It modifies the scored
// segments in place.
func DeflectionFilter(sw *ScoredWay)
```

- Algorithm:
  1. Iterate through segments sequentially by index.
  2. For each segment with a non-zero score (`Score > 0`):
     a. Start a look-ahead window from this segment.
     b. Accumulate segments forward until the cumulative distance reaches `DeflectionLookAheadM` (2400m) or the end of the way.
     c. Compute the overall bearing change: the absolute difference between the bearing at the start of the window (bearing from `window[0].Start` to `window[0].End`) and the bearing at the end of the window (bearing from `window[last].Start` to `window[last].End`). Use `geo.Bearing()` and `geo.AngleDiff()`.
     d. If the overall bearing change is less than `DeflectionMinHeadingChange` (20°), zero out all segments in the window: set `Score = 0`, `Tier = 0`, `Weight = 0` for each.
     e. Advance the index past the end of the window (skip processed segments).
  3. If the segment's score is already zero, skip it and advance.

- Implement a batch function:

```go
// ApplyDeflectionFilter applies the deflection filter to all scored ways.
func ApplyDeflectionFilter(ways []ScoredWay)
```

This calls `DeflectionFilter` on each way (by pointer).

### 2. Write unit tests in `quality/deflection_test.go`

- **Straight road with dogleg**: Create a way that goes north, jogs east briefly, then continues north. The jog segments should have their scores zeroed because the overall bearing change over 2.4km is minimal.
- **Genuinely winding road**: Create a way with sustained heading changes (e.g., a series of alternating left-right turns accumulating to >20° overall). Scores should be preserved.
- **Short way (< 2.4km)**: The look-ahead window uses whatever segments remain. If the short way is straight with a dogleg, scores should still be zeroed.
- **All-zero segments**: A way with all tier-0 segments should pass through unchanged.
- **Mixed way**: First half is straight with a dogleg (should be zeroed), second half is genuinely winding (should be preserved).
- **Single segment**: A way with one scored segment where the window can only include that one segment — verify behavior is reasonable.
- Verify that zeroed segments have `Tier = 0`, `Weight = 0`, and `Score = 0`.

### 3. Test helper for building synthetic ways

- Create a helper function in the test file that builds a `ScoredWay` from a sequence of `geo.Coord` points with pre-computed segments (using real Haversine lengths and bearings). This avoids coupling deflection tests to the scoring implementation.

## Acceptance Criteria

- [ ] `quality/deflection.go` exists with `DeflectionFilter` and `ApplyDeflectionFilter`.
- [ ] `quality/deflection_test.go` passes with `go test ./quality/...`.
- [ ] A straight road with an intersection dogleg has its curvature zeroed.
- [ ] A genuinely winding road retains its curvature scores.
- [ ] Zeroed segments have tier 0, weight 0, and score 0.
- [ ] The filter uses `DeflectionLookAheadM` and `DeflectionMinHeadingChange` constants.
- [ ] The filter skips segments that already have zero scores.

## Notes

- The bearing at the "start of the window" is the bearing of the first segment in the window. The bearing at the "end of the window" is the bearing of the last segment in the window. Use `geo.Bearing(seg.Start, seg.End)` for each.
- `geo.AngleDiff()` already handles the 360°/0° wraparound correctly (returns values in [0, 180]).
- The filter advances past the window after processing it — this prevents double-processing segments that were already zeroed.
- These values (2.4km, 20°) are from the Curvature project and should be noted in comments.

---
# Task 005 Review: Deflection Filter

**Reviewer:** Senior Software Engineer Agent
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task implements a look-ahead deflection filter (`DeflectionFilter`) that zeroes out curvature scores for segments representing minor heading deviations within an otherwise straight 2.4 km window, plus a batch wrapper `ApplyDeflectionFilter`. This is stage 4 of the scoring pipeline.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/deflection.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/deflection_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `quality/deflection.go` exists with `DeflectionFilter` and `ApplyDeflectionFilter` | PASS |
| `quality/deflection_test.go` passes with `go test ./quality/...` | PASS |
| A straight road with an intersection dogleg has its curvature zeroed | PASS |
| A genuinely winding road retains its curvature scores | PASS |
| Zeroed segments have tier 0, weight 0, and score 0 | PASS |
| The filter uses `DeflectionLookAheadM` and `DeflectionMinHeadingChange` constants | PASS |
| The filter skips segments that already have zero scores | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test   # all packages pass (quality: 8.940s)
make lint   # go vet ./... — no issues reported
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass, tests pass, linter is clean. Previous review issues (window advance logic, `latOffset` documentation, preserved-segment field assertion) were all addressed in post-review fixes and are confirmed resolved in the current code.
