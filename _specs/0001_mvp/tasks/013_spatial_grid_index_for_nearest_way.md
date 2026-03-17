---
# Task 013: Spatial Grid Index for NearestWay

## Summary

Replace the O(ways × nodes_per_way) brute-force scan in `quality.NearestWay` with a
pre-built spatial grid index. Each way node is bucketed into a lat/lon cell at index-build
time. At query time only the 3×3 neighborhood of cells around the query point is searched,
reducing the average number of Haversine calls per query by two orders of magnitude.

## Dependencies

Task 007 (ApplyQuality / NearestWay exists), Task 011 (timing instrumentation).

## Background / Findings

Performance analysis (Task 012) used a standalone micro-benchmark with realistic
Asheville→Knoxville parameters (3 routes, 5,000 points/route, 6,000 ways, 8 nodes/way):

| Approach | elapsed_ms | Haversine calls | Speedup |
|----------|-----------|-----------------|---------|
| Baseline brute-force | 22,660 ms | 720 M | 1× |
| Spatial grid 0.02° cells | 682 ms | ~21 M | 33× |
| Spatial grid 0.01° cells | 189 ms | ~6 M | 120× |

The grid index produces **identical results** to the brute-force scan at both cell sizes
(same `found` count in every run). The index builds in < 5 ms.

The `apply-quality` stage dominates total pipeline time. `fetch-ways` and all other stages
are measured at < 3,000 ms combined for this corridor.

## Detailed Directions

### 1. Define the `SpatialGrid` type in `quality/grid.go` (new file)

```go
package quality

import "math"

// SpatialGrid partitions Way nodes into fixed-size lat/lon cells so that
// NearestWay can restrict its search to the 3×3 neighborhood of any query point.
type SpatialGrid struct {
    cells    map[[2]int][]*Way // keyed by (cellX, cellY)
    cellSize float64           // cell edge length in degrees
}

// BuildSpatialGrid indexes all nodes of every way into a grid with cells of
// cellSizeDeg degrees on each side. Recommended value: 0.01 (≈ 1.1 km).
func BuildSpatialGrid(ways []Way, cellSizeDeg float64) *SpatialGrid { ... }

// NearestWayGrid returns the nearest Way within maxDist meters of p, searching
// only the 3×3 cell neighborhood. Returns nil if none is found within maxDist.
func (g *SpatialGrid) NearestWayGrid(p geo.Coord, maxDist float64) *Way { ... }
```

Implementation notes:
- `cellKey(c geo.Coord) [2]int` — `{int(math.Floor(c.Lon/cellSize)), int(math.Floor(c.Lat/cellSize))}`
- During build, de-duplicate: if the same `*Way` pointer would be added to a cell more
  than once (because multiple nodes of that way fall in the same cell), add it only once.
  Use a `map[*Way]bool` scratch-pad per way.
- During query, iterate the 3×3 neighborhood (`dx, dy ∈ {-1,0,1}`), skip cells that don't
  exist in the map, and de-duplicate way pointers across cells with a local `map[*Way]bool`
  before inner-node iteration.
- Keep `NearestWay` (the brute-force function) in place — it is used in tests and as a
  correctness reference.

### 2. Update `ApplyQuality` in `quality/overpass.go`

Replace the `NearestWay(midpoint, ways, maxDist)` call with:

```go
w := grid.NearestWayGrid(midpoint, maxDist)
```

Build the grid once before the route loop:

```go
const gridCellDeg = 0.01
grid := BuildSpatialGrid(ways, gridCellDeg)
```

The grid build takes < 5 ms and is amortized across all routes.

### 3. Add unit tests in `quality/grid_test.go`

- **TestBuildSpatialGrid_Empty**: zero ways → grid with no cells, query returns nil.
- **TestNearestWayGrid_ExactMatch**: single way with one node at a known point; query at
  that exact point with maxDist=1 → returns that way.
- **TestNearestWayGrid_MaxDistRespected**: nearest way is 100 m away; query with
  maxDist=50 → returns nil; query with maxDist=200 → returns the way.
- **TestNearestWayGrid_MatchesBruteForce**: generate 200 random ways (5 nodes each) in a
  0.5°×0.5° bounding box; for 500 random query points, assert that
  `NearestWayGrid` and `NearestWay` return the same `*Way` pointer (or both nil).

### 4. Verify the speedup

Build the binary and run:

```
./twisty \
  -origin "Asheville, NC" \
  -dest "Knoxville, TN" \
  -twist 0.8 \
  -v \
  2>timing_after.log
```

Check that `apply-quality elapsed_ms` in `timing_after.log` is ≤ 5,000 ms.

## Acceptance Criteria

- [ ] `quality/grid.go` is added with `SpatialGrid`, `BuildSpatialGrid`, and
  `NearestWayGrid`.
- [ ] `ApplyQuality` uses the grid index instead of brute-force `NearestWay`.
- [ ] All existing `quality` tests continue to pass.
- [ ] `TestNearestWayGrid_MatchesBruteForce` passes (grid and brute-force agree on 500
  random queries).
- [ ] `apply-quality elapsed_ms` ≤ 5,000 ms for the Asheville→Knoxville test route
  (down from ~135,000 ms with the brute-force baseline measured on 2026-03-16).
- [ ] `go test ./...` passes with no regressions.

## Trade-offs and Accuracy Implications

- **Accuracy**: The 3×3 cell search is conservative. At a cell size of 0.01° (≈ 1.1 km),
  the search radius covers ≈ 3.3 km around the query point, which comfortably exceeds
  `maxDist = 30 m`. A way node could theoretically be missed only if it lies more than
  1.1 km from the query point AND is somehow the nearest way — impossible when `maxDist`
  is 30 m. The `MatchesBruteForce` test verifies this empirically.
- **Memory**: The grid adds one pointer per (way, cell) pair. With 6,000 ways × 8 nodes,
  that is at most 48,000 pointers (≈ 384 KB at 8 bytes each). Negligible.
- **Incremental complexity**: ~100 lines of new Go code, no external dependencies.

## Notes

**Live run data captured 2026-03-16** using a Go integration test with real network calls
(OSRM + Overpass API). Asheville, NC (35.5951, -82.5515) → Knoxville, TN (35.9606, -83.9207):

```
stage=fetch-routes  elapsed_ms=527    routes=2   total_points=4499
stage=fetch-ways    elapsed_ms=8251   ways=80849
stage=apply-quality elapsed_ms=134944 routes=2   ways=80849  total_points=4499
```

The real corridor returns **80,849 ways** — roughly 13.5× more than the synthetic benchmark
assumed (6,000 ways). The brute-force `apply-quality` takes ~135 s on this corridor, not
the ~22 s estimated by the micro-benchmark (the benchmark was calibrated to a smaller way
set). The grid index is proportionally more critical than originally estimated.

The acceptance-criterion threshold has been updated from ≤ 2,000 ms to ≤ 5,000 ms to
reflect the real way count. At 0.01° cell size the grid reduces Haversine calls by ~120×,
so 135,000 ms / 120 ≈ 1,125 ms is the theoretical minimum; 5,000 ms gives comfortable
headroom for map-overhead at 80k+ ways.

Synthetic benchmark (Apple M-series, 6,000 ways × 8 nodes, 3 routes × 5,000 pts):

```
stage=apply-quality elapsed_ms=22660 ways=6000 total_points=15000   # baseline
Spatial grid 0.01°:  build_ms=4  query_ms=185  total_ms=189          # 120× faster
Spatial grid 0.02°:  build_ms=2  query_ms=680  total_ms=682          # 33× faster
```

Both grid sizes produce identical `found` counts to the brute-force baseline.
The recommended cell size of 0.01° gives the best query time while keeping cell count
well within a Go map's practical range.
