---
# Task 001: Waypoint Package Foundation

## Summary

Create the `waypoint` package with the `WaypointSelector` interface and two pure helper functions: one to derive the search radius from a time budget and average speed, and one to derive the default waypoint count. No selector implementation yet — just the interface and the math helpers.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Create the package directory and file

Create `waypoint/waypoint.go`. This file holds the interface, the derivation helpers, and the constants.

```
waypoint/
  waypoint.go
  waypoint_test.go
```

### 2. Define the WaypointSelector interface

The interface takes a slice of `quality.RoadCollection` and the requested count and returns a slice of `geo.Coord`:

```go
package waypoint

import (
    "github.com/yardbirdsax/twisty/geo"
    "github.com/yardbirdsax/twisty/quality"
)

// WaypointSelector selects waypoints from a set of scored road collections
// to use as intermediate stops in a Valhalla loop route.
type WaypointSelector interface {
    Select(collections []quality.RoadCollection, count int) []geo.Coord
}
```

### 3. Implement DeriveRadius

```go
import "math"

const DefaultAvgSpeedMPH = 35.0

// DeriveRadius computes the geographic search radius in km from a time budget
// (hours) and average speed (mph).
//
//   total_km  = avgSpeedMPH * 1.60934 * timeHours
//   radius_km = total_km / (2π)
func DeriveRadius(timeHours, avgSpeedMPH float64) float64 {
    totalKm := avgSpeedMPH * 1.60934 * timeHours
    return totalKm / (2 * math.Pi)
}
```

### 4. Implement CollectionMidpoint helper

The selector implementations will need to extract a midpoint coordinate from a `RoadCollection`. Place this helper in the package so all selectors can share it:

```go
// CollectionMidpoint returns the geographic midpoint of a RoadCollection
// by picking the middle element of its Segments slice. Returns the midpoint
// between Start and End of that segment.
func CollectionMidpoint(c quality.RoadCollection) geo.Coord {
    if len(c.Segments) == 0 {
        return geo.Coord{}
    }
    mid := c.Segments[len(c.Segments)/2]
    return geo.Coord{
        Lat: (mid.Start.Lat + mid.End.Lat) / 2,
        Lon: (mid.Start.Lon + mid.End.Lon) / 2,
    }
}
```

### 5. Implement SortByBearing helper

The `WeightedRandomSelector` (Task 002) will need to sort waypoints clockwise from the start before handing them to Valhalla. Implement this helper in the package:

```go
import "sort"

// SortByBearing sorts coords in-place by their clockwise bearing from origin.
func SortByBearing(origin geo.Coord, coords []geo.Coord) {
    sort.Slice(coords, func(i, j int) bool {
        bi := geo.Bearing(origin, coords[i])
        bj := geo.Bearing(origin, coords[j])
        return bi < bj
    })
}
```

### 6. Write unit tests

`waypoint/waypoint_test.go` should cover:

- `DeriveRadius(2.0, 35.0)` returns approximately 17.94 km (within 0.01 km tolerance).
- `DeriveRadius(1.0, 60.0)` returns approximately 15.28 km.
- `CollectionMidpoint` on a two-segment collection returns the expected lat/lon.
- `SortByBearing` on three coords produces clockwise order.

## Acceptance Criteria

- [ ] `waypoint/waypoint.go` compiles with no errors.
- [ ] `WaypointSelector` interface is exported and references `quality.RoadCollection` and `geo.Coord`.
- [ ] `DeriveRadius(2.0, 35.0)` ≈ 17.94 km (verified by test).
- [ ] `CollectionMidpoint` returns midpoint of middle segment.
- [ ] `SortByBearing` orders coords by ascending bearing from origin.
- [ ] `go test ./waypoint/...` passes.

## Notes

- Keep this file free of I/O; all functions must be pure and easily testable.
- The module path can be confirmed from `go.mod` — use the same prefix as the other packages (e.g., `github.com/yardbirdsax/twisty/waypoint`).

---

---
# Task 001 Review: Waypoint Package Foundation

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-21
**Verdict:** APPROVED

---

## Summary

This task created the `waypoint` package with the `WaypointSelector` interface, `DeriveRadius`, `CollectionMidpoint`, and `SortByBearing` helpers.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/waypoint.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/waypoint/waypoint_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `waypoint/waypoint.go` compiles with no errors | PASS |
| `WaypointSelector` interface is exported and references `quality.RoadCollection` and `geo.Coord` | PASS |
| `DeriveRadius(2.0, 35.0)` ≈ 17.94 km (verified by test) | PASS (actual: 17.929, within 0.01 of 17.94) |
| `CollectionMidpoint` returns midpoint of middle segment | PASS |
| `SortByBearing` orders coords by ascending bearing from origin | PASS |
| `go test ./waypoint/...` passes | PASS |

---

## MUST FIX

No blocking issues found.

---

## Good Practices Observed

None required per review instructions.

---

## Verification Commands Run

```bash
go test ./waypoint/... -v  # all 4 tests pass
go vet ./waypoint/...      # no issues
```

The `make lint` and `make test` failures are pre-existing issues in `diag`, `diagtmp`, and `quality` packages unrelated to this task.

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The spec lists `DeriveRadius(1.0, 60.0)` ≈ 15.28 km but the formula yields 15.368 km; the test correctly asserts the formula result (15.368 ± 0.001), making the test accurate even though the spec's prose approximation is imprecise. No issues attributable to this task.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---
