# Task 002: Geo Math Package

## Summary

Implement the `geo` package with the `Coord` type and the three pure math functions needed throughout the pipeline: haversine distance, bearing, and angle difference.

## Dependencies

Task 001

## Detailed Directions

### 1. Define the Coord Type

In `geo/geo.go`:

```go
package geo

type Coord struct {
    Lat float64
    Lon float64
}
```

### 2. Implement Haversine Distance

```go
// Haversine returns the great-circle distance in meters between two coordinates.
func Haversine(a, b Coord) float64
```

Formula (R = 6371000 meters):

```
dLat = (b.Lat - a.Lat) * π/180
dLon = (b.Lon - a.Lon) * π/180
sinDLat = math.Sin(dLat / 2)
sinDLon = math.Sin(dLon / 2)
h = sinDLat*sinDLat + math.Cos(a.Lat*π/180)*math.Cos(b.Lat*π/180)*sinDLon*sinDLon
return 2 * 6371000 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
```

### 3. Implement Bearing

```go
// Bearing returns the initial bearing in degrees [0, 360) from a to b.
func Bearing(a, b Coord) float64
```

Formula:

```
dLon = (b.Lon - a.Lon) * π/180
y    = math.Sin(dLon) * math.Cos(b.Lat*π/180)
x    = math.Cos(a.Lat*π/180)*math.Sin(b.Lat*π/180) -
       math.Sin(a.Lat*π/180)*math.Cos(b.Lat*π/180)*math.Cos(dLon)
bearing = math.Atan2(y, x) * 180/π
return math.Mod(bearing+360, 360)
```

### 4. Implement Angle Difference

```go
// AngleDiff returns the absolute difference between two bearings, handling
// 360°/0° wraparound. Result is in [0, 180].
func AngleDiff(a, b float64) float64
```

Algorithm:

```
diff = b - a
for diff > 180  { diff -= 360 }
for diff < -180 { diff += 360 }
return math.Abs(diff)
```

### 5. Write Unit Tests

Create `geo/geo_test.go`. Include tests for:

- `Haversine`: known distance (e.g., SF to LA is approximately 559 km; accept ±1%).
- `Bearing`: north (0°), east (90°), south (180°), west (270°) at the equator.
- `AngleDiff`: wraparound cases — e.g., diff between 350° and 10° should be 20°, not 340°.

## Acceptance Criteria

- [ ] `go test ./geo/` passes with all tests green
- [ ] `Haversine` returns a value within 1% of the known SF→LA great-circle distance (~559 km)
- [ ] `Bearing` returns correct cardinal directions at the equator
- [ ] `AngleDiff(350, 10)` returns 20.0
- [ ] `AngleDiff(10, 350)` returns 20.0
- [ ] All functions use `float64` throughout

## Notes

- Use `import "math"` only; no external packages.
- All angles in the public API are in degrees; convert to radians internally.

---
# Task 002 Review: Geo Math Package

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

This task implemented the `geo` package with the `Coord` struct and three pure math functions: `Haversine`, `Bearing`, and `AngleDiff`, along with a unit test file covering all required cases.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/geo.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/geo_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go test ./geo/` passes with all tests green | PASS |
| `Haversine` returns a value within 1% of the known SF→LA great-circle distance (~559 km) | PASS |
| `Bearing` returns correct cardinal directions at the equator | PASS |
| `AngleDiff(350, 10)` returns 20.0 | PASS |
| `AngleDiff(10, 350)` returns 20.0 | PASS |
| All functions use `float64` throughout | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Formula fidelity:** Implementation matches the specified formulas exactly, including the correct `math.Mod(bearing+360, 360)` normalization.

---

## Verification Commands Run

```bash
go test ./geo/   # ok  github.com/yardbirdsax/twisty/geo  0.206s
go vet ./geo/    # no output (clean)
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met, tests pass, and `go vet` reports no issues.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
