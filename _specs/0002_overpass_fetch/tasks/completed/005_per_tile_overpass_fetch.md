# Task 005: Per-Tile Overpass Fetching with Cache Integration

## Summary

Implement the per-tile Overpass fetch function and the orchestration loop that iterates over tiles, checks cache, fetches missing tiles with retry logic, and respects rate limits. This is the core fetch engine of the tiled pipeline.

## Dependencies

Task 001 (ID field on Way/overpassElement), Task 002 (tile grid computation), Task 003 (retry helper), Task 004 (tile cache).

## Detailed Directions

### 1. Implement `fetchTileRaw`

- In `quality/tilefetch.go`, write a stateless per-tile fetch function:
  ```go
  func fetchTileRaw(ctx context.Context, endpoint string, t Tile) ([]byte, error)
  ```
- Behavior:
  1. Construct the Overpass query using the existing `HighwayFilter` constant and the tile's bounding box. The query format should match `fetchWaysFromURL` but return raw bytes:
     ```
     [out:json][timeout:25];
     way["highway"~"<HighwayFilter>"](south,west,north,east);
     out geom;
     ```
  2. Create an HTTP POST request with `context` attached (use `http.NewRequestWithContext`).
  3. Send the request using the existing `overpassHTTPClient`.
  4. If the response status indicates a transient error (429, 5xx), return a normal error (retryable).
  5. If the response status is a non-retryable client error (4xx other than 429), return `NonRetryable(err)`.
  6. If status is 200, read and return the full response body as raw bytes.
  7. Do NOT parse the JSON here — raw bytes go to cache.

### 2. Implement `FetchTiledWays` orchestrator

- Write the main orchestration function:
  ```go
  type TileFetchConfig struct {
      Endpoint    string        // Overpass API base URL (default: overpassBaseURL)
      TileSize    float64       // degrees (default: 0.05)
      Cache       *TileCache
      NoCache     bool          // skip cache reads, still write
      Logger      *slog.Logger
  }

  func FetchTiledWays(ctx context.Context, centerLat, centerLon, radiusKm float64, cfg TileFetchConfig) ([]Way, error)
  ```
- Behavior:
  1. Call `ComputeTiles(centerLat, centerLon, radiusKm, cfg.TileSize)` to get the tile list.
  2. Ensure cache directory exists via `cfg.Cache.EnsureDir()`.
  3. Log total tile count.
  4. For each tile:
     a. If `!cfg.NoCache` and `cfg.Cache.Has(tile)`, read from cache. Log cache hit (at debug level). Read the cached bytes.
     b. Otherwise, call `retryWithBackoff` wrapping `fetchTileRaw`:
        ```go
        var raw []byte
        err := retryWithBackoff(ctx, 3, 2*time.Second, func() error {
            var fetchErr error
            raw, fetchErr = fetchTileRaw(ctx, cfg.Endpoint, tile)
            return fetchErr
        })
        ```
     c. On success, write to cache via `cfg.Cache.Write(tile, raw)`.
     d. On failure (all retries exhausted), log a warning with tile coordinates and error, then continue to the next tile.
     e. After a successful fetch (not cache hit), sleep 1 second before the next tile (rate limiting). Use a context-aware sleep.
  5. After all tiles are processed, merge and deduplicate (call the merge function from Task 006, or collect raw bytes and defer merging).
  6. Log summary: total tiles, cache hits, fetched, failed.

### 3. Implement context-aware sleep

- Write a helper for the inter-request delay:
  ```go
  func sleepWithContext(ctx context.Context, d time.Duration) error {
      select {
      case <-time.After(d):
          return nil
      case <-ctx.Done():
          return ctx.Err()
      }
  }
  ```

### 4. Write unit tests

- In `quality/tilefetch_test.go`:
  - `TestFetchTileRawSuccess`: Use `httptest.NewServer` returning valid Overpass JSON. Verify raw bytes are returned and contain expected content.
  - `TestFetchTileRawHTTP429Retryable`: Return 429, verify the error is retryable (not wrapped in NonRetryable).
  - `TestFetchTileRawHTTP400NonRetryable`: Return 400, verify the error is non-retryable.
  - `TestFetchTiledWaysCacheHit`: Pre-populate cache with tile data, call `FetchTiledWays`, verify no HTTP requests are made (use a request counter on the httptest server).
  - `TestFetchTiledWaysNoCache`: Set `NoCache: true`, verify tiles are fetched even when cache exists, and cache is updated.
  - `TestFetchTiledWaysPartialFailure`: Mock server that fails for one specific tile but succeeds for others. Verify partial results are returned and the failed tile is logged.
  - `TestFetchTiledWaysRateLimit`: Verify at least 1 second passes between consecutive fetch requests (use timestamps).

## Acceptance Criteria

- [x] `fetchTileRaw` sends correct Overpass query for a tile's bounding box
- [x] HTTP errors are correctly classified as retryable vs. non-retryable
- [x] `FetchTiledWays` checks cache before fetching
- [x] Successful fetches are written to cache
- [x] Failed tiles (after retries) are skipped with a warning, not fatal
- [x] 1-second delay between consecutive fetches
- [x] `NoCache` flag bypasses cache reads but still writes
- [x] All unit tests pass
- [x] `context.Context` is threaded through all fetch functions

## Notes

- The `endpoint` parameter on `fetchTileRaw` allows injection of an httptest URL for testing, following the same pattern as `fetchWaysFromURL`.
- The 1-second delay is applied by the orchestrator, not the fetch function, per the PRD's concurrency-readiness principle.
- The `FetchTiledWays` function returns `[]Way` for downstream consumption. The raw-to-Way parsing and deduplication is covered in Task 006, but the orchestrator calls it.

---
# Task 005 Review: Per-Tile Overpass Fetching with Cache Integration

**Reviewer:** Claude (claude-sonnet-4-6)
**Date:** 2026-03-19
**Verdict:** APPROVED

---

## Summary

This task implements `fetchTileRaw` (stateless per-tile HTTP fetch), `FetchTiledWays` (orchestration loop with cache integration, retry, rate limiting, and deduplication), `sleepWithContext` (context-aware delay), and the full test suite for all of these.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/quality/tilefetch_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `fetchTileRaw` sends correct Overpass query for a tile's bounding box | PASS |
| HTTP errors are correctly classified as retryable vs. non-retryable | PASS |
| `FetchTiledWays` checks cache before fetching | PASS |
| Successful fetches are written to cache | PASS |
| Failed tiles (after retries) are skipped with a warning, not fatal | PASS |
| 1-second delay between consecutive fetches | PASS |
| `NoCache` flag bypasses cache reads but still writes | PASS |
| All unit tests pass | PASS |
| `context.Context` is threaded through all fetch functions | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Verification Commands Run

```bash
make test              # all tests pass (quality package: 3.640s)
make lint              # go vet reports no issues
go test -race ./quality/...  # passes cleanly under race detector (4.659s)
```

---

## Final Verdict

**APPROVED**

All three previously-reported issues are confirmed fixed: `fetched++` is now after `sleepWithContext` (line 302), `TestFetchTiledWaysPartialFailure` uses HTTP 400 (non-retryable, instant failure), and `TestFetchTiledWaysRateLimit` protects `requestTimes` with `sync.Mutex`. All acceptance criteria pass. Tests pass under `go test -race`.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
