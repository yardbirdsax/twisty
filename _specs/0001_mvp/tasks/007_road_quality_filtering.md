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

- [ ] go build ./... succeeds
- [ ] IsDisqualifying returns true for access=private, highway=track, motor_vehicle=no
- [ ] PenaltyFactor returns correct values for each surface category
- [ ] When Overpass is unavailable, the program prints the fallback warning and continues
- [ ] AdjustedScore is less than Score when a route has disqualifying segments
- [ ] Warning line is printed when disqualifiedFraction greater than 0.10

## Notes

- quality package imports route for route.Route. Be careful of import cycles: route must NOT import quality.
- The 30m proximity threshold is a heuristic; exact precision is not required.
- Overpass can be slow on large bounding boxes; the 10-second timeout is a hard limit.
- **Go 1.26:** This project requires Go 1.26. Use `io.ReadAll(resp.Body)` to read the Overpass HTTP response body — `io.ReadAll` received a ~2× speedup and ~50% allocation reduction in this release compared to earlier versions.
