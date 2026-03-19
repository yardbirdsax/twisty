# Task 006: Tile Merge and Way Deduplication

## Summary

Implement the merge layer that parses raw Overpass JSON from cached tile files, combines ways from all tiles, and deduplicates by OSM way ID. This produces the final `[]Way` slice consumed by downstream pipeline stages.

## Dependencies

Task 001 (Way struct with ID field), Task 004 (tile cache for reading files).

## Detailed Directions

### 1. Implement `parseTileData`

- In `quality/tilefetch.go`, write a function to parse raw Overpass JSON bytes into Way objects:
  ```go
  func parseTileData(data []byte) ([]Way, error)
  ```
- Behavior:
  1. Unmarshal `data` into an `overpassResponse` struct (the existing type).
  2. Convert each `overpassElement` to a `Way`, including the `ID` field.
  3. Return the slice of Ways.
- This reuses the same conversion logic as `fetchWaysFromURL` but operates on raw bytes instead of making an HTTP request. Consider extracting the element-to-Way conversion from `fetchWaysFromURL` into a shared helper if it reduces duplication, but avoid modifying the existing function's signature.

### 2. Implement `mergeAndDeduplicate`

- Write:
  ```go
  func mergeAndDeduplicate(tileData [][]byte, logger *slog.Logger) ([]Way, error)
  ```
- Behavior:
  1. Initialize a `map[int64]struct{}` for tracking seen way IDs.
  2. Initialize a `[]Way` result slice.
  3. For each tile's raw bytes:
     a. Call `parseTileData` to get the tile's ways.
     b. For each way, check if its ID is in the seen map.
     c. If not seen, add to result and mark as seen.
     d. If seen (duplicate), skip it.
  4. Log (at info level): `"merged %d ways from %d tiles (%d duplicates removed)"`.
  5. Return the deduplicated slice.

### 3. Handle ways with ID 0

- Ways with `ID == 0` (which shouldn't happen with Overpass `out geom`, but could indicate a parsing issue) should be included without deduplication — do not use 0 as a dedup key, as it would incorrectly merge unrelated ways. Log a warning if any ID-0 ways are encountered.

### 4. Wire merge into `FetchTiledWays`

- In the `FetchTiledWays` orchestrator (from Task 005), after collecting all raw tile data (from cache reads and fresh fetches), call `mergeAndDeduplicate` and return the result.

### 5. Write unit tests

- In `quality/tilefetch_test.go`:
  - `TestParseTileDataBasic`: Parse a minimal Overpass JSON response with 2 ways, verify Way fields (ID, Tags, Geometry).
  - `TestParseTileDataEmpty`: Parse a response with no elements, verify empty slice returned (not nil).
  - `TestParseTileDataMalformed`: Parse invalid JSON, verify error returned.
  - `TestMergeAndDeduplicateNoDuplicates`: Two tiles with distinct ways, verify all ways present.
  - `TestMergeAndDeduplicateWithDuplicates`: Two tiles sharing a way (same ID), verify it appears only once in output.
  - `TestMergeAndDeduplicateZeroID`: Ways with ID 0 are all included (not deduped against each other).
  - `TestMergeAndDeduplicatePreservesGeometry`: Verify that the full geometry is preserved for deduplicated ways (not truncated or modified).

## Acceptance Criteria

- [ ] `parseTileData` correctly converts raw Overpass JSON to `[]Way` with ID, Tags, and Geometry
- [ ] `mergeAndDeduplicate` removes duplicate ways by OSM ID
- [ ] Ways with ID 0 are not deduplicated against each other
- [ ] Merge is wired into `FetchTiledWays` orchestrator
- [ ] Verbose logging reports total ways, tiles, and duplicates removed
- [ ] All unit tests pass

## Notes

- Deduplication is by ID only. The PRD explicitly states: "Do not attempt to merge or reconcile differing tag sets."
- The first-encountered instance of a duplicate way is kept. Since Overpass returns identical data for the same way in different tiles, the choice is arbitrary.
- Performance note: For a 25 km radius with 0.05° tiles, expect ~400 tiles with potentially thousands of ways each. The dedup map should use `int64` keys (not string) for efficiency.
