# Task 005: Implement Cache Validity Checking

## Summary

Implement logic to determine whether a per-region cache file is still valid (i.e., was generated from the current PBF file and doesn't need re-conversion). This enables the pipeline to skip conversion for regions whose cache is already up-to-date.

## Dependencies

Task 004

## Detailed Directions

### 1. Define Cache Metadata

Create a mechanism to track which PBF file a cache was generated from. Options:

**Option A: Sidecar metadata file**
- For each cache file `{region}.osm.gz`, write `{region}.osm.gz.meta` containing the source PBF's size and modification time.
- On validity check, stat the PBF and compare against the stored metadata.

**Option B: Derive validity from file existence + PBF mtime**
- A cache file is valid if: it exists, and its mtime is newer than the source PBF's mtime.
- Simpler but less robust (system clock changes could invalidate).

Choose the approach specified in the design document (Task 003). Option A is more robust.

### 2. Implement Validity Check Function

```go
func isCacheValid(pbfPath string, cachePath string) (bool, error)
```

- Returns `true` if the cache file exists and was generated from the current version of the PBF.
- Returns `false` if the cache is missing, stale, or the PBF has changed.
- Returns an error only for unexpected I/O failures (not for "cache miss" conditions).

### 3. Implement Cache Metadata Writing

- After `convertSingleRegionToCache` succeeds, write the metadata (PBF size + mtime) alongside the cache file.
- This should be called from the conversion function or its caller.

### 4. Write Tests

- Test: fresh cache (just converted) is valid.
- Test: missing cache file returns invalid.
- Test: cache from a different PBF version (simulated by changing PBF mtime) returns invalid.
- Test: corrupt or missing metadata file returns invalid.

## Acceptance Criteria

- [ ] `isCacheValid` correctly identifies valid caches
- [ ] `isCacheValid` correctly identifies stale/missing caches
- [ ] Metadata is written atomically alongside cache files
- [ ] Tests cover valid, missing, and stale scenarios
- [ ] No false positives (stale cache never reported as valid)

## Notes

- The validity check must be cheap (stat calls only, no file content reading).
- Consider using `encoding/json` for the metadata sidecar for simplicity and debuggability.
- `twisty overpass clean` should delete the entire cache directory (including metadata files) — verify this in Task 007.
