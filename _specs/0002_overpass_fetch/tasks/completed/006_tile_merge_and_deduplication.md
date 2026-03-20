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

---
# Task 006 Review: Tile Merge and Way Deduplication

**Reviewer:** Claude (Sonnet 4.6)
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task implemented `parseTileData` and `mergeAndDeduplicate` in `quality/tilefetch.go`, extracted a shared `elementsToWays` helper into `quality/overpass.go`, wired the merge into `FetchTiledWays`, and added all seven required unit tests.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/overpass.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `parseTileData` correctly converts raw Overpass JSON to `[]Way` with ID, Tags, and Geometry | PASS |
| `mergeAndDeduplicate` removes duplicate ways by OSM ID | PASS |
| Ways with ID 0 are not deduplicated against each other | PASS |
| Merge is wired into `FetchTiledWays` orchestrator | PASS |
| Verbose logging reports total ways, tiles, and duplicates removed | PASS |
| All unit tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test   # all packages pass: quality 4.082s
make lint   # no issues (go vet only, no output)
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. The shared `elementsToWays` helper eliminates the duplication that was flagged in the prior review pass. The log message matches the spec's prescribed format. All seven required unit tests are present and pass. No issues remain.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
