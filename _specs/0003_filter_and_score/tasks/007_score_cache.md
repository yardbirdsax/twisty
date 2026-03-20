# Task 007: Score Cache

## Summary

Implement a per-tile score cache that stores the output of stages 2–4 (hard filter, scoring, deflection filter) so that subsequent runs skip recomputation when inputs haven't changed. The cache invalidates when raw tile data or scoring parameters change.

## Dependencies

- Task 001 (scoring parameters — `ScoringParamsHash`)
- Task 004 (segment scoring — `ScoredWay`, `ScoredSegment`)
- Task 006 (score pipeline — `RunScorePipeline`)

## Detailed Directions

### 1. Create `quality/scorecache.go`

- Create the file `quality/scorecache.go` with package `quality`.
- Define the cache file structure:

```go
// ScoreCacheEntry is the on-disk format for a cached tile's scored segments.
type ScoreCacheEntry struct {
    RawTileHash string      `json:"raw_tile_hash"`
    ParamsHash  string      `json:"params_hash"`
    Ways        []ScoredWay `json:"ways"`
}
```

- Ensure `ScoredWay` and `ScoredSegment` have JSON tags (update the structs in `curvature.go` if needed):
  - `ScoredWay`: `way_id`, `segments`
  - `ScoredSegment`: `start`, `end`, `radius`, `tier`, `weight`, `length`, `score`
  - `geo.Coord`: check if it already has JSON tags; if not, add `lat` and `lon` tags to the `Coord` struct in `geo/geo.go`.

- Define the `ScoreCache` type:

```go
// ScoreCache manages cached scoring results per tile.
type ScoreCache struct {
    Dir       string
    Precision int
}
```

- Implement methods mirroring `TileCache` patterns:

```go
func (c *ScoreCache) EnsureDir() error
func (c *ScoreCache) Path(t Tile) string              // same key format as TileCache
func (c *ScoreCache) Has(t Tile) bool
func (c *ScoreCache) ClearAll() error
```

- Implement the core read/write methods:

```go
// Read loads a cache entry and validates both hashes. Returns the cached
// scored ways and true if valid, or nil and false if the cache misses.
func (c *ScoreCache) Read(t Tile, rawTileData []byte) ([]ScoredWay, bool)

// Write stores scored ways along with the current raw tile hash and params hash.
func (c *ScoreCache) Write(t Tile, rawTileData []byte, ways []ScoredWay) error
```

- Implement the hashing helper:

```go
// hashBytes returns "sha256:<hex>" for the given data.
func hashBytes(data []byte) string
```

- `Read` logic:
  1. Check if cache file exists. If not, return `nil, false`.
  2. Read and unmarshal the JSON.
  3. Compute `hashBytes(rawTileData)` and compare to `entry.RawTileHash`.
  4. Compute `ScoringParamsHash()` and compare to `entry.ParamsHash`.
  5. If both match, return `entry.Ways, true`.
  6. Otherwise return `nil, false`.

- `Write` logic:
  1. Build a `ScoreCacheEntry` with `hashBytes(rawTileData)`, `ScoringParamsHash()`, and the scored ways.
  2. Marshal to JSON.
  3. Write atomically (temp file → rename), following `TileCache.Write` pattern.

### 2. Cache directory location

- The score cache directory is a sibling to the raw tile cache: if the tile cache is at `~/.twisty/cache/overpass/`, the score cache is at `~/.twisty/cache/scores/`.
- The cache key format matches the tile cache: `tile_<south>_<west>.json`.

### 3. Write unit tests in `quality/scorecache_test.go`

- **Write and read back**: Write a cache entry, then read it with the same raw tile data. Should return the cached ways.
- **Raw tile data changed**: Write a cache entry, then read with different raw tile data. Should return `false`.
- **Params hash changed**: This is harder to test directly since params are constants. Instead, test `hashBytes` independently and verify that `Read` checks the params hash by writing a cache file with a manually constructed wrong params hash.
- **Cache miss (no file)**: Read from an empty cache. Should return `nil, false`.
- **ClearAll**: Write entries, clear all, verify reads return false.
- **Atomic write**: Verify the file exists after `Write` and contains valid JSON.
- **Path format**: Verify `Path()` returns expected format matching tile cache keys.

## Acceptance Criteria

- [ ] `quality/scorecache.go` exists with `ScoreCache`, `ScoreCacheEntry`, and all methods.
- [ ] `quality/scorecache_test.go` passes with `go test ./quality/...`.
- [ ] Cache hits return stored data when both hashes match.
- [ ] Cache misses when raw tile data changes.
- [ ] Cache misses when params hash doesn't match.
- [ ] `ClearAll` removes all cache entries.
- [ ] Writes are atomic (temp file → rename).
- [ ] JSON serialization round-trips correctly for all scored segment fields.

## Notes

- The `ScoreCache` is structurally very similar to `TileCache` — follow the same patterns for `EnsureDir`, `Path`, atomic writes, etc.
- `geo.Coord` may need JSON tags added — this is a small, safe change. Check the existing struct first.
- `+Inf` does not serialize cleanly in JSON. Handle this by encoding infinite radius as `-1` or a sentinel value in JSON, and converting back on read. Alternatively, use `math.MaxFloat64` as the sentinel.
