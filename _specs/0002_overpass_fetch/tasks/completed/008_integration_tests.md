# Task 008: Integration Tests

## Summary

Write integration tests that verify the full tiled fetch pipeline end-to-end, using httptest mock servers for both Overpass and Nominatim APIs. These tests validate that all components (tile computation, cache, fetch, retry, merge, dedup) work together correctly.

## Dependencies

All prior tasks (001–007).

## Detailed Directions

### 1. Create integration test file

- Create `quality/tilefetch_integration_test.go` for tests that exercise the full pipeline.
- Use `httptest.NewServer` to mock the Overpass API, returning realistic JSON responses keyed by the bounding box in the POST body.

### 2. Test: Full fetch with empty cache

- `TestTileFetchIntegrationEmptyCache`:
  1. Set up an httptest Overpass server that:
     - Parses the bounding box from each POST body.
     - Returns a JSON response with 2-3 ways per tile, some shared across adjacent tiles (to test dedup).
     - Tracks request count.
  2. Call `FetchTiledWays` with a small radius (e.g., 2 km, tile size 0.05°) to keep tile count low.
  3. Assert:
     - All tiles were fetched (request count matches expected tile count).
     - Returned ways are deduplicated (shared ways appear once).
     - Cache files exist on disk for each tile.

### 3. Test: Second run uses cache

- `TestTileFetchIntegrationCacheHit`:
  1. Run `FetchTiledWays` once to populate cache (same setup as above).
  2. Reset the request counter.
  3. Run `FetchTiledWays` again with the same parameters.
  4. Assert:
     - Zero HTTP requests made (all from cache).
     - Returned ways are identical to the first run.
     - Cache file mtimes were updated.

### 4. Test: Overlapping queries share cache

- `TestTileFetchIntegrationOverlap`:
  1. Fetch with center A and small radius.
  2. Record which tiles were fetched (track via request log).
  3. Fetch with center B (shifted slightly so some tiles overlap).
  4. Assert:
     - Only non-overlapping tiles triggered new HTTP requests.
     - Overlapping tiles were served from cache.

### 5. Test: `--no-cache` flag re-fetches

- `TestTileFetchIntegrationNoCache`:
  1. Run once to populate cache.
  2. Run again with `NoCache: true`.
  3. Assert:
     - All tiles were re-fetched (request count matches tile count).
     - Cache files were updated (new content written).

### 6. Test: Partial failure resilience

- `TestTileFetchIntegrationPartialFailure`:
  1. Set up a mock server that returns 500 for one specific tile (identified by bbox) and 200 for all others.
  2. Call `FetchTiledWays`.
  3. Assert:
     - Ways from successful tiles are returned.
     - No cache file exists for the failed tile.
     - The function does not return an error (partial results are OK).

### 7. Test: Retry behavior on transient error

- `TestTileFetchIntegrationRetry`:
  1. Set up a mock server that returns 429 for the first 2 requests to a specific tile, then 200.
  2. Call `FetchTiledWays`.
  3. Assert:
     - The tile is eventually fetched successfully.
     - Request count for that tile is 3 (2 retries + 1 success).
     - Cache file exists for the tile.
  - Use short retry delays (override or use a test-specific config) to keep the test fast.

### 8. Test: Way ID deduplication accuracy

- `TestTileFetchIntegrationDedup`:
  1. Set up mock responses where the same way (same ID, same geometry) appears in 3 different tiles.
  2. Call `FetchTiledWays`.
  3. Assert:
     - The way appears exactly once in the output.
     - Its geometry is complete (not clipped).

## Acceptance Criteria

- [ ] All integration tests pass
- [ ] Tests use httptest mock servers (no real API calls)
- [ ] Tests use `t.TempDir()` for cache isolation
- [ ] Cache hit/miss behavior verified across multiple runs
- [ ] Overlapping query cache sharing verified
- [ ] Partial failure returns partial results without error
- [ ] Retry behavior verified with transient errors
- [ ] Deduplication accuracy verified with shared ways
- [ ] Tests complete in under 30 seconds (using short delays)

## Notes

- These tests are slower than unit tests due to the multi-tile orchestration, but should still be fast since they use mocks and short delays.
- Consider using `testing.Short()` to skip these in `-short` mode if needed.
- The mock Overpass server should be simple — it doesn't need to implement real spatial queries. Just return pre-built responses with known way IDs based on which tile is being requested (parse the bbox from the POST body).
- For retry tests, use a much shorter initial delay (e.g., 10ms) via the config to avoid slow tests.

---
# Task 008 Review: Integration Tests

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

Integration tests for the full tiled fetch pipeline are implemented in `quality/tilefetch_integration_test.go`. All seven required test functions are present, using httptest mock servers and `t.TempDir()` for isolation. All previously identified issues have been resolved.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch_integration_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/Makefile` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| All integration tests pass | PASS |
| Tests use httptest mock servers (no real API calls) | PASS |
| Tests use `t.TempDir()` for cache isolation | PASS |
| Cache hit/miss behavior verified across multiple runs | PASS |
| Overlapping query cache sharing verified | PASS |
| Partial failure returns partial results without error | PASS |
| Retry behavior verified with transient errors | PASS |
| Deduplication accuracy verified with shared ways | PASS |
| Tests complete in under 30 seconds (using short delays) | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test                                                          # PASS (short mode, integration tests skipped)
make lint                                                          # clean
go test -v -run "TestTileFetchIntegration" ./quality/...          # all 7 tests PASS
# Overlap test output: cache_hits=1 fetched=3 on second run — bOnly assertion exercised
```

---

## Final Verdict

**APPROVED**

All acceptance criteria are met. All seven integration tests pass, use httptest mock servers, use `t.TempDir()` for isolation, and complete in well under 30 seconds. The overlap test correctly exercises the B-only and overlapping tile cache assertions. The NoCache test verifies mtime updates. The retry test confirms the 3-request sequence. Ready to merge.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
