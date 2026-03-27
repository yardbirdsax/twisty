---
# Task 003 Review: FetchLoopRoute with Multi-Leg Decoding Fix

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-21
**Verdict:** APPROVED

---

## Summary

Adds `FetchLoopRoute` to the `route` package in a new file `route/valhalla_loop.go`. Extracts a `stitchLegs` helper that decodes each Valhalla leg's polyline independently and stitches the coordinate slices with correct boundary-point deduplication. Includes unit tests covering two-leg stitching, single-leg equivalence, empty-leg error handling, non-200 HTTP errors, and Valhalla error-body surfacing. The existing `FetchRoutes` / `tripToRoute` path is left untouched.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/valhalla_loop.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/valhalla_loop_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/route/valhalla.go` | Reviewed (unchanged) |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `FetchLoopRoute` is exported from the `route` package | PASS |
| Two-leg stitching is correct (no duplicate boundary point, no garbled coords) | PASS |
| `Route.Duration` equals sum of per-leg `Summary.Time` values | PASS |
| `Route.Distance` equals sum of per-leg `Summary.Length` * 1000 | PASS |
| `ScoreRoute` is called before the route is returned | PASS |
| Existing `FetchRoutes` tests continue to pass (`go test ./route/...`) | PASS |
| Unit test for `stitchLegs` passes with synthetic polyline data | PASS |

---

## MUST FIX

No blocking issues found.

---

## Good Practices Observed

None noted.

---

## Verification Commands Run

```bash
go test -short ./route/...  # PASS — all route package tests pass
go vet ./route/...           # PASS — no issues
make test                    # route package PASS; failures in diag/quality/diagtmp are pre-existing and unrelated to this task
make lint                    # sandbox permission error on go build cache — unrelated to code quality
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The `stitchLegs` helper correctly decodes each leg independently at precision `1e6` and deduplicates the boundary point. Duration and distance accumulation sums per-leg values with the correct km-to-meters conversion. `ScoreRoute` is called before return. All route package tests pass and `go vet` is clean.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.

---
# Task 003: FetchLoopRoute with Multi-Leg Decoding Fix

## Summary

Add `FetchLoopRoute` to the `route` package. This function issues a single Valhalla `/route` request with the pattern `[start, wp1, …, wpN, start]` and correctly stitches the multi-leg response by decoding each leg's polyline independently before concatenating coordinates. The existing `FetchRoutes` / `tripToRoute` path is left untouched.

## Dependencies

Task 001 (for `geo.Coord` familiarity; no code dependency — `geo` already exists).

## Detailed Directions

### 1. Read existing route/valhalla.go carefully

Before writing any code, read `route/valhalla.go` in full, paying attention to:
- The `valhallaRequest` and `valhallaResponse` struct definitions.
- How `tripToRoute` calls `geo.DecodePolyline` with precision `1e6`.
- The known bug at line ~79 where it concatenates raw encoded leg strings.

### 2. Create route/valhalla_loop.go

All new code lives in a new file to avoid touching the existing path:

```go
package route

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/yardbirdsax/twisty/geo"
)

// FetchLoopRoute issues a Valhalla /route request for a loop:
// [start, wp1, wp2, …, wpN, start].
// Each leg is decoded independently before stitching, avoiding the
// multi-leg polyline encoding bug in tripToRoute.
func FetchLoopRoute(start geo.Coord, waypoints []geo.Coord) (Route, error)
```

### 3. Build the request

Construct a `valhallaRequest` (reuse the existing struct from `valhalla.go`) with:
- Locations: `[start] + waypoints + [start]`  — N+2 locations total.
- Costing: `"auto"` with `"use_highways": 0.3` (same as `FetchRoutes`).
- `"alternates": 0` — no alternate routes needed.

If the existing `valhallaRequest` struct is unexported or tightly coupled, replicate only the fields needed here. Prefer reuse.

### 4. Decode legs independently

The `valhallaTrip` struct has a `Legs []valhallaLeg` field (or similar). After parsing the response:

```go
var allPoints []geo.Coord
for i, leg := range trip.Legs {
    pts := geo.DecodePolyline(leg.Shape, 1e6)
    if i == 0 {
        allPoints = append(allPoints, pts...)
    } else {
        // Drop the first point of each subsequent leg — it duplicates
        // the last point of the previous leg.
        if len(pts) > 0 {
            allPoints = append(allPoints, pts[1:]...)
        }
    }
}
```

### 5. Compute duration and distance

Sum `leg.Summary.Time` and `leg.Summary.Length` across all legs (checking actual field names in the existing structs). Populate `Route.Duration` and `Route.Distance`.

### 6. Score the route

Call `ScoreRoute(&r)` on the assembled route before returning, consistent with how `FetchRoutes` produces scored routes.

### 7. Handle errors

- HTTP non-200: return a descriptive error including the status code.
- Empty legs or zero points: return an error, not a zero-value Route.
- Valhalla error body (contains `"error"` key): surface the Valhalla error message.

### 8. Write unit tests

`route/valhalla_loop_test.go`:

- Construct a synthetic `valhallaResponse` with two legs, each having an independently delta-encoded polyline shape. Verify that `stitchLegs` (extract the stitching logic into a small testable helper) returns the correct merged coordinate slice without duplicates at the boundary.
- Verify that a single-leg response (two locations, one leg) produces the same result as the existing decoding path.

You do not need an integration test against a live Valhalla instance; unit-test the decoding logic only.

## Acceptance Criteria

- [ ] `FetchLoopRoute` is exported from the `route` package.
- [ ] For a two-leg response, coordinates from leg 1 and leg 2 are correctly stitched (no duplicate boundary point, no garbled coordinates from raw concatenation).
- [ ] `Route.Duration` equals the sum of per-leg `Summary.Time` values.
- [ ] `Route.Distance` equals the sum of per-leg `Summary.Length` values (converted to meters if Valhalla returns km).
- [ ] `ScoreRoute` is called before the route is returned.
- [ ] The existing `FetchRoutes` tests continue to pass (`go test ./route/...`).
- [ ] Unit test for `stitchLegs` (or equivalent) passes with synthetic polyline data.

## Notes

- Valhalla encodes polylines at precision `1e6` (6 decimal places), not the standard Google `1e5`. Verify this matches the existing call in `tripToRoute`.
- Valhalla returns distance in km; `Route.Distance` is in meters — multiply by 1000.
- The boundary-point deduplication rule: the last point of leg N equals the first point of leg N+1 because Valhalla starts every leg at the previous leg's endpoint. Dropping `pts[1:]` (i.e., keeping `pts[0]` from leg 0 only) is correct.

---
