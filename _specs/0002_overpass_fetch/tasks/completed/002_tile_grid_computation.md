# Task 002: Tile Grid Computation

## Summary

Implement the tile grid computation logic that converts a center point (lat, lon) and radius (km) into a deterministic set of tile coordinates aligned to a global grid. This is the spatial decomposition layer that all subsequent fetching and caching operates on.

## Dependencies

Task 001 (for the Way struct with ID field, though this task doesn't directly use Way).

## Detailed Directions

### 1. Create the tile data model

- In a new file `quality/tilefetch.go`, define a `Tile` struct:
  ```go
  type Tile struct {
      South float64
      West  float64
      North float64
      East  float64
  }
  ```
- The tile is identified by its `(South, West)` corner. `North` and `East` are computed as `South + tileSize` and `West + tileSize`.

### 2. Implement `snapToGrid`

- Write a helper function `snapToGrid(coord, tileSize float64) float64` that snaps a coordinate down to the nearest tile boundary:
  ```go
  func snapToGrid(coord, tileSize float64) float64 {
      return math.Floor(coord/tileSize) * tileSize
  }
  ```

### 3. Implement `ComputeTiles`

- Write `ComputeTiles(centerLat, centerLon, radiusKm, tileSizeDeg float64) []Tile`:
  1. Convert `radiusKm` to degree offsets:
     - Latitude offset: `radiusKm / 111.32`
     - Longitude offset: `radiusKm / (111.32 * math.Cos(centerLat * math.Pi / 180.0))`
  2. Compute the bounding box:
     - `south = centerLat - latOffset`
     - `north = centerLat + latOffset`
     - `west = centerLon - lonOffset`
     - `east = centerLon + lonOffset`
  3. Snap outward to tile boundaries:
     - `gridSouth = snapToGrid(south, tileSizeDeg)`
     - `gridWest = snapToGrid(west, tileSizeDeg)`
     - `gridNorth` = snap north up: use `snapToGrid(north, tileSizeDeg) + tileSizeDeg` if north doesn't fall exactly on a boundary, or `snapToGrid(north, tileSizeDeg)` if it does. Simplest: iterate while `lat < north`.
     - Same for `gridEast`.
  4. Iterate from `gridSouth` to just below `gridNorth` in `tileSizeDeg` increments, and from `gridWest` to just below `gridEast` in `tileSizeDeg` increments, producing a `Tile` for each cell.
  5. Return the slice of tiles.

### 4. Implement coordinate formatting for cache keys

- Write `TileCacheKey(t Tile, precision int) string` that formats tile coordinates into a deterministic string suitable for file names:
  ```go
  func TileCacheKey(t Tile, precision int) string {
      format := fmt.Sprintf("%%.%df", precision)
      south := fmt.Sprintf(format, t.South)
      west := fmt.Sprintf(format, t.West)
      return fmt.Sprintf("tile_%s_%s.json", south, west)
  }
  ```
- Default precision of 3 decimal places for 0.05° tiles.

### 5. Write unit tests

- In `quality/tilefetch_test.go`, write tests:
  - `TestSnapToGrid`: Verify snapping for positive, negative, and exact-boundary values.
  - `TestComputeTilesSingleTile`: A very small radius that fits in one tile.
  - `TestComputeTilesMultipleTiles`: A larger radius producing a known grid (e.g., center at 42.36, -72.58 with 5 km radius and 0.05° tiles).
  - `TestComputeTilesDeterministic`: Same inputs always produce the same tiles.
  - `TestComputeTilesShiftedCenter`: Two calls with slightly different centers but overlapping areas produce overlapping tile sets (verifying global grid alignment).
  - `TestTileCacheKey`: Verify formatting with positive and negative coordinates.

## Acceptance Criteria

- [ ] `Tile` struct defined with South, West, North, East fields
- [ ] `snapToGrid` correctly snaps coordinates to tile boundaries
- [ ] `ComputeTiles` returns a complete set of tiles covering the circle's bounding box
- [ ] Tile grid is globally aligned (not relative to query center)
- [ ] `TileCacheKey` produces deterministic, filesystem-safe file names
- [ ] All unit tests pass
- [ ] Overlapping queries share tiles (verified by test)

## Notes

- Use `math.Floor` for snapping, not `math.Round` — we always want to snap outward to ensure full coverage.
- Floating-point precision is important. The formatting function should use fixed decimal places (e.g., `%.3f`) to avoid inconsistencies.
- The tile size default of 0.05° is ~5.5 km at mid-latitudes. At 25 km radius, this produces roughly 20×20 = 400 tiles (a reasonable number for sequential fetching).

---
# Task 002 Review: Tile Grid Computation

**Reviewer:** Claude (senior-software-engineer)
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task implements the spatial decomposition layer: a `Tile` struct, `snapToGrid` helper, `ComputeTiles` function (center + radius to global-aligned tile set), and `TileCacheKey` for deterministic cache file names. A previous review pass raised two SHOULD FIX items (custom `abs` helper and weak tile-count assertion); both have been resolved in the current implementation.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `Tile` struct defined with South, West, North, East fields | PASS |
| `snapToGrid` correctly snaps coordinates to tile boundaries | PASS |
| `ComputeTiles` returns a complete set of tiles covering the circle's bounding box | PASS |
| Tile grid is globally aligned (not relative to query center) | PASS |
| `TileCacheKey` produces deterministic, filesystem-safe file names | PASS |
| All unit tests pass | PASS |
| Overlapping queries share tiles (verified by test) | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test   # all packages pass: quality ok 0.464s
make lint   # go vet ./... — no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass, all tests are green, and the linter is clean. Previous SHOULD FIX items (custom `abs` helper replaced with `math.Abs`, tile-count assertion tightened to exact value of 9) have been addressed. The implementation is ready for the next task.
