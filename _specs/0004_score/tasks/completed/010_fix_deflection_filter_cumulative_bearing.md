---
# Task 010: Fix Deflection Filter to Use Cumulative Bearing Change

## Summary

The deflection filter currently computes the **net** bearing change between the first and last segment in the look-ahead window. This incorrectly zeroes out scores on winding roads (S-curves, switchbacks) where the heading returns to roughly the same direction. The fix is to compute **cumulative** bearing change — the sum of absolute bearing differences between every consecutive pair of segments in the window — which is what the Curvature project does.

## Dependencies

None — this is a bug fix to existing code.

## Detailed Directions

### 1. Update the Deflection Filter Algorithm

In `quality/deflection.go`, replace the net bearing change calculation with a cumulative one.

**Current (broken):**
```go
startBearing := geo.Bearing(segs[i].Start, segs[i].End)
endBearing := geo.Bearing(segs[last].Start, segs[last].End)
bearingChange := geo.AngleDiff(startBearing, endBearing)
```

**Correct approach:**
- Iterate through all segments in the window `[i, windowEnd)`
- For each consecutive pair of segments `(j, j+1)`, compute the bearing of each segment and the absolute angle difference between them
- Sum all those absolute differences to get the cumulative heading change
- Compare the cumulative total against `DeflectionMinHeadingChange` (20°)

```go
// Compute cumulative heading change across the window.
cumBearingChange := 0.0
for j := i; j < windowEnd-1; j++ {
    bj := geo.Bearing(segs[j].Start, segs[j].End)
    bk := geo.Bearing(segs[j+1].Start, segs[j+1].End)
    cumBearingChange += geo.AngleDiff(bj, bk)
}

if cumBearingChange < DeflectionMinHeadingChange {
    // Zero out the window...
}
```

- Keep the rest of the function logic unchanged: the sliding window approach, the zero-out behavior, and the advance-past-window optimization all remain the same.

### 2. Update the Comment/Docstring

- Update the function docstring to accurately describe "cumulative heading change" rather than "overall bearing change"
- The comment at the top of `scoring_params.go` for `DeflectionMinHeadingChange` should also be updated if it references "overall" or "net" change

### 3. Update Existing Tests

- Review `quality/deflection_test.go` for any tests that assert on the net-bearing behavior
- Update test expectations where needed: roads with S-curves that previously had scores zeroed should now retain them
- Add a specific test case for an S-curve road:
  - A way that curves left 90° then right 90° over ~2.4 km (net change ~0°, cumulative change ~180°)
  - This should **pass** the deflection filter (cumulative change 180° > 20° threshold) and retain its scores
  - With the old net-bearing implementation, this would have been incorrectly zeroed

### 4. Add a Test Case for Genuinely Straight Roads

- Ensure the filter still works correctly for its intended purpose: zeroing out minor deflections on essentially straight roads
- Test a way with many segments over 2.4 km where each segment deviates by only 1-2° from the previous (cumulative ~15°)
- This should still be zeroed out by the filter

### 5. Clear Score Cache

- Since the deflection filter runs during scoring (stage 3), cached scores reflect the old (broken) filter
- Add a note in the PR description that users must run `twisty score --clear-score-cache` after upgrading
- The `ScoringParamsHash` does not need to change since the parameters themselves haven't changed — only the algorithm using them

## Acceptance Criteria

- [ ] `DeflectionFilter` computes cumulative (sum of consecutive absolute bearing diffs) rather than net bearing change
- [ ] S-curve test case: a road with 180° cumulative heading change over 2.4 km retains its scores
- [ ] Straight-road test case: a road with < 20° cumulative heading change over 2.4 km is still zeroed
- [ ] All existing tests pass (updated as needed for new behavior)
- [ ] `go test -race ./quality/...` passes

## Notes

- This is the most likely root cause for known-curvy roads (e.g., Birchrun Rd) showing mostly green in KML output while the Curvature project shows them as orange
- The Curvature project (Python) sums heading changes across all segment pairs in the window; our implementation was incorrectly using only the net change between window endpoints
- This fix will change scores for many roads, so existing KML output will differ after the fix — this is expected and desired
- Performance impact is negligible: the inner loop adds O(window_size) bearing calculations per window, which is the same order as the existing loop that accumulates distance

---
# Task 010 Review: Fix Deflection Filter to Use Cumulative Bearing Change

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-20
**Verdict:** APPROVED

---

## Summary

This task replaced the net (start-to-end) bearing change calculation in `DeflectionFilter` with a cumulative sum of absolute bearing differences between every consecutive pair of segments in the look-ahead window. The fix correctly handles S-curves and switchbacks that return to their original heading.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/deflection.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/deflection_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/scoring_params.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `DeflectionFilter` computes cumulative (sum of consecutive absolute bearing diffs) rather than net bearing change | PASS |
| S-curve test case: a road with 180° cumulative heading change over 2.4 km retains its scores | PASS |
| Straight-road test case: a road with < 20° cumulative heading change over 2.4 km is still zeroed | PASS |
| All existing tests pass (updated as needed for new behavior) | PASS |
| `go test -race ./quality/...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
make test   # all packages pass
make lint   # no lint issues (go vet clean)
go test -race ./quality/...  # ok, no race conditions
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. The cumulative bearing algorithm is correctly implemented, the docstring is accurate, and the test suite covers S-curves, gentle deviations, short ways, single segments, and mixed ways.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.
