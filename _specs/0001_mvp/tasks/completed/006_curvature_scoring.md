# Task 006: Curvature Scoring

## Summary

Implement curvature scoring in `route/score.go`: compute indirectness ratio, angular density, and combined score for each decoded route polyline.

## Dependencies

Task 002 (geo.Haversine, geo.Bearing, geo.AngleDiff), Task 005 (route.Route)

## Detailed Directions

### 1. Define CurvatureStats

In `route/score.go` (or move to a shared file within the `route` package):

```go
package route

type CurvatureStats struct {
    Indirectness   float64 // straight-line / road distance (0-1; lower = more indirect)
    AngularDensity float64 // degrees of heading change per km
    Score          float64 // combined curvature score (higher = twistier)
    AdjustedScore  float64 // score after road quality penalties (set in Task 007)
}
```

If `CurvatureStats` was stubbed in Task 005, replace the stub with this definition.

### 2. Implement ScoreRoute

```go
// ScoreRoute computes curvature statistics for a decoded route polyline.
// It populates route.Stats.Indirectness, AngularDensity, and Score.
func ScoreRoute(r *Route)
```

**Total road distance:**
```
totalDist = sum of geo.Haversine(points[i], points[i+1]) for i in 0..n-2
```

**Indirectness:**
```
straightLine = geo.Haversine(points[0], points[n-1])
indirectness = straightLine / totalDist
// Clamp to [0, 1] to guard against floating-point edge cases
```

If `totalDist == 0`, set all stats to 0 and return early.

**Angular density:**
```
totalHeadingChange = 0
for i = 1; i < len(points)-1; i++:
    b1 = geo.Bearing(points[i-1], points[i])
    b2 = geo.Bearing(points[i],   points[i+1])
    totalHeadingChange += geo.AngleDiff(b1, b2)

angularDensity = totalHeadingChange / (totalDist / 1000)  // °/km
```

**Combined score:**
```
score = angularDensity * 0.7 + (1.0 - indirectness) * 1000 * 0.3
```

**Set AdjustedScore = Score** as a default (road quality in Task 007 will update it).

### 3. Implement ScoreAll

```go
// ScoreAll calls ScoreRoute on each route in the slice.
func ScoreAll(routes []Route)
```

### 4. Write Unit Tests

Create `route/score_test.go`:

- **Straight line test:** 3 points in a straight line (e.g., same longitude, increasing latitude). `AngularDensity` should be 0 or very close to 0.
- **Right-angle test:** 3 points forming a 90° turn. `AngleDiff` at the middle point should be 90°.
- **Indirectness test:** For points forming a semicircle, `Indirectness` should be significantly less than 1.
- **Zero-length guard:** A route with 0 or 1 points should not panic.

## Acceptance Criteria

- [ ] `go test ./route/ -run TestScore` passes
- [ ] A straight 3-point route has `AngularDensity` of 0 (or < 0.01 due to floating point)
- [ ] A 90° turn route has a total heading change of 90°
- [ ] `Score` is always non-negative
- [ ] `AdjustedScore` equals `Score` after `ScoreRoute` (before road quality is applied)
- [ ] Route with fewer than 2 points does not panic

## Notes

- Guard against `totalDist == 0` (identical points) to avoid division by zero.
- Guard against fewer than 3 points when computing angular density (the loop simply won't execute, producing `angularDensity = 0`, which is correct).
- `AdjustedScore` is initialized here and will be overwritten in Task 007.

---
# Task 006 Review: Curvature Scoring

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

Implements `CurvatureStats`, `ScoreRoute`, and `ScoreAll` in `route/score.go`, plus unit tests in `route/score_test.go`. Computes indirectness ratio, angular density, and a combined curvature score for each route polyline.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/score.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/score_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/osrm.go` | Reviewed (Route type) |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go test ./route/ -run TestScore` passes | PASS |
| Straight 3-point route has `AngularDensity` < 0.01 | PASS |
| 90-degree turn route has total heading change of 90 degrees | PASS |
| `Score` is always non-negative | PASS |
| `AdjustedScore` equals `Score` after `ScoreRoute` | PASS |
| Route with fewer than 2 points does not panic | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
go test ./route/ -run TestScore -v -count=1  # all 5 tests PASS
go vet ./...                                  # no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass, implementation is correct, linter is clean, and all previously noted test gaps have been addressed.

---
