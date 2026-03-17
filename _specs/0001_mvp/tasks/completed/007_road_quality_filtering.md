# Task 007: Road Quality Filtering via Overpass API

## Summary

Implement the quality package: query Overpass for highway ways within the combined bounding box of all candidate routes, match route points to nearby ways, compute disqualified/penalized fractions, and apply score adjustments. Overpass failure must not block output.

## Dependencies

Task 002 (geo.Coord, geo.Haversine), Task 006 (route.Route, route.CurvatureStats.AdjustedScore)

## Detailed Directions

### 1. Define the Way Type

In quality/overpass.go:

```go
package quality

import "github.com/yardbirdsax/twisty/geo"

type Way struct {
    Tags     map[string]string
    Geometry []geo.Coord
}
```

### 2. Implement Bounding Box Computation

```go
// BoundingBox returns (south, west, north, east) covering all points in all routes,
// expanded by bufferDeg on each side.
func BoundingBox(routes [][]geo.Coord, bufferDeg float64) (south, west, north, east float64)
```

Walk all coordinates to find min/max lat and lon. Subtract bufferDeg from min, add to max.

### 3. Implement FetchWays

```go
// FetchWays queries Overpass for all highway ways within the bounding box.
// Returns (ways, nil) on success, or (nil, error) on any failure.
func FetchWays(south, west, north, east float64) ([]Way, error)
```

POST to https://overpass-api.de/api/interpreter with Content-Type application/x-www-form-urlencoded.
Body: data equals url-encoded query
Query includes JSON output, timeout, way selection, and geometry output.

Use an http.Client with a 10-second timeout.

Parse response with Elements containing Tags and Geometry fields.

### 4. Implement Way Tag Classification

```go
// IsDisqualifying returns true if the way's tags indicate the route segment
// should be heavily penalized (private, restricted, or non-motor-vehicle road).
func IsDisqualifying(tags map[string]string) bool

// PenaltyFactor returns a soft penalty factor in [0, 1] for unpaved or
// low-quality surfaces. 0 = no penalty, 1 = full soft penalty.
func PenaltyFactor(tags map[string]string) float64
```

Disqualifying conditions (any one is true):
- tags["access"] is "private" or "no"
- tags["highway"] is track, path, footway, or cycleway
- tags["motor_vehicle"] is "no" or "private"

Penalty factors:
- surface is unpaved, gravel, dirt, mud, or sand returns 1.0
- surface is compacted or fine_gravel returns 0.5
- highway is unclassified and no surface tag returns 0.25
- highway is service returns 0.25
- Otherwise returns 0.0

### 5. Implement Point-to-Way Matching

```go
// NearestWay returns the Way whose geometry has a node within maxDist meters
// of point p, or nil if none is found.
func NearestWay(p geo.Coord, ways []Way, maxDist float64) *Way
```

Brute-force: iterate all ways, iterate all geometry nodes, compute geo.Haversine(p, node). Return the way with minimum distance if it is less than or equal to maxDist.

### 6. Implement ApplyQuality

```go
// ApplyQuality checks each route against the fetched ways and updates
// route.Stats.AdjustedScore. It also prints warnings for heavily disqualified routes.
// If ways is nil (Overpass failed), it prints the fallback warning and returns immediately.
func ApplyQuality(routes []route.Route, ways []Way)
```

For each route:
1. Walk adjacent point pairs
2. Find the nearest way to the midpoint of each segment using maxDist of 30 meters
3. Accumulate disqualifiedDist and penaltyWeightedDist
4. Compute fractions based on totals
5. Apply adjustments to score based on fractions
6. If disqualifiedFraction greater than 0.10, print warning

Fallback when ways is nil: Print warning about Overpass API unavailable

## Acceptance Criteria

- [x] go build ./... succeeds
- [x] IsDisqualifying returns true for access=private, highway=track, motor_vehicle=no
- [x] PenaltyFactor returns correct values for each surface category
- [x] When Overpass is unavailable, the program prints the fallback warning and continues
- [x] AdjustedScore is less than Score when a route has disqualifying segments
- [x] Warning line is printed when disqualifiedFraction greater than 0.10

## Notes

- quality package imports route for route.Route. Be careful of import cycles: route must NOT import quality.
- The 30m proximity threshold is a heuristic; exact precision is not required.
- Overpass can be slow on large bounding boxes; the 10-second timeout is a hard limit.
- **Go 1.26:** This project requires Go 1.26. Use `io.ReadAll(resp.Body)` to read the Overpass HTTP response body — `io.ReadAll` received a ~2× speedup and ~50% allocation reduction in this release compared to earlier versions.

---
# Task 007 Review: Road Quality Filtering via Overpass API

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

Implements the `quality` package with `BoundingBox`, `FetchWays`, `IsDisqualifying`, `PenaltyFactor`, `NearestWay`, and `ApplyQuality`. All core logic matches the spec. The `quality` package correctly imports `route` without creating an import cycle.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/overpass.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/overpass_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| go build ./... succeeds | PASS |
| IsDisqualifying returns true for access=private, highway=track, motor_vehicle=no | PASS |
| PenaltyFactor returns correct values for each surface category | PASS |
| When Overpass is unavailable, the program prints the fallback warning and continues | PASS |
| AdjustedScore is less than Score when a route has disqualifying segments | PASS |
| Warning line is printed when disqualifiedFraction greater than 0.10 | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Testable internal helper:** `fetchWaysFromURL` accepts an endpoint parameter, enabling httptest-based tests without monkey-patching globals.
2. **Pointer stability in NearestWay:** `for i := range ways` with `&ways[i]` avoids returning a pointer to a loop-copy variable.
3. **Nil-ways fallback:** `ApplyQuality` exits cleanly and prints the required warning when `ways` is nil, ensuring Overpass failure never blocks output.

---

## Verification Commands Run

```bash
go build ./...              # success, no errors
go vet ./...                # no issues
go test ./quality/... -v    # all 25 tests PASS
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, all 25 quality-package tests pass, `go build` and `go vet` are clean. No issues requiring revision.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
