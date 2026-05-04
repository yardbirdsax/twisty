# Task 007: Implement Incremental Merge Orchestrator

## Summary

Implement `convertRegionsIncremental`, the orchestrator function that replaces `convertPBFsToBZ2` in `runOverpassStart`. This function checks per-region cache validity, converts only stale regions, and merges all cached regions through the parallel BZ2 writer into the final `merged.osm.bz2`.

## Dependencies

Task 005, Task 006

## Detailed Directions

### 1. Implement `convertRegionsIncremental`

In `overpass.go` (or a helper file if cleaner):

```go
func convertRegionsIncremental(allRegions []string, dataDir string, progress osmconv.ConvertProgress) error
```

Logic:
1. Create `{dataDir}/cache/` directory if it doesn't exist.
2. For each region:
   a. Compute PBF path and cache path.
   b. Call `isCacheValid()` — if valid, skip; if stale, call `convertSingleRegionToCache()`.
   c. Report progress (e.g., "Region 3/5: delaware (cached)" or "Region 3/5: vermont (converting...)").
3. Clean orphan cache files (see section 2).
4. Merge all cache files into `merged.osm.bz2` using `mergeRegionCachesToBZ2()`.

### 2. Implement Orphan Cache Cleanup

When regions are removed from the set, their cache files become orphans. During the orchestration step:

```go
func cleanOrphanCacheFiles(cacheDir string, activeRegions []string) error
```

- List all `.osm.gz` files in `cacheDir`.
- Build a set of expected cache filenames from `activeRegions`.
- Delete any `.osm.gz` file (and its `.meta.json` sidecar) not in the expected set.
- Log which files were cleaned up.

### 3. Implement `mergeRegionCachesToBZ2`

```go
func mergeRegionCachesToBZ2(cachePaths []string, bz2Path string, progress osmconv.ConvertProgress) error
```

- Open each cache file as a `GzipXMLScanner` (from Task 004).
- Create the output file with `ParallelBZ2Writer` (from Task 006) wrapping an `XMLWriter`.
- Call `osmconv.Convert` with the scanners and writer.
- Close all writers and scanners.

### 4. Wire Into `runOverpassStart`

Replace the existing `convertPBFsToBZ2` call in `runOverpassStart` with `convertRegionsIncremental`:

**Before:**
```go
if _, err := os.Stat(mergedBZ2); os.IsNotExist(err) {
    // ... build pbfPaths ...
    convertPBFsToBZ2(pbfPaths, mergedBZ2, progress)
}
```

**After:**
```go
if _, err := os.Stat(mergedBZ2); os.IsNotExist(err) {
    convertRegionsIncremental(allRegions, dataDir, progress)
}
```

### 5. Update Region-Change Logic

Currently, when regions change, `runOverpassStart` deletes `merged.osm.bz2` and `db/`. This logic remains, but now it preserves the `cache/` directory — only `merged.osm.bz2` and `db/` are deleted. The per-region caches are preserved and reused.

Verify that the existing region-change detection (lines 192-206 of `overpass.go`) does NOT delete `{dataDir}/cache/`.

### 6. Write Tests

- **Full pipeline test**: Mock or use small PBF files, run `convertRegionsIncremental`, verify `merged.osm.bz2` is produced and decompresses to valid OSM XML.
- **Cache reuse test**: Run twice with same regions — second run should skip all conversions (only merge).
- **Incremental addition test**: Run with regions A, B. Then run with A, B, C — only C should be converted.
- **Orphan cleanup test**: Run with A, B, C. Then run with A, B — C's cache files should be deleted.
- **Progress reporting test**: Verify progress callbacks indicate which regions are cached vs. converting.

## Acceptance Criteria

- [ ] `convertRegionsIncremental` correctly orchestrates per-region caching and merging
- [ ] Cached regions are skipped on subsequent runs (only new regions are converted)
- [ ] Orphan cache files are cleaned up when regions are removed
- [ ] Final `merged.osm.bz2` is a valid BZ2 file with correct merged/deduplicated OSM XML
- [ ] `runOverpassStart` uses the new incremental pipeline
- [ ] Region-change logic preserves the cache directory
- [ ] Tests pass

## Notes

- The CPU profiling (`--cpuprofile`) flag should continue to work with the new pipeline — the profiling start/stop wraps the `convertRegionsIncremental` call just as it wrapped `convertPBFsToBZ2`.
- Progress reporting may need adjustment — the existing `ConvertProgress` interface reports bytes read from PBF files, but the merge step reads from gzip XML caches. Consider whether to report cache-reading progress or just the final merge progress.
- Keep `convertPBFsToBZ2` as a private function for now — it may be useful as a fallback or for testing. Remove it later if unused.

---

# Task 007 Review: Implement Incremental Merge Orchestrator

**Reviewer:** Principal Engineer
**Date:** 2026-05-03
**Verdict:** APPROVED

---

## Summary

Implements `convertRegionsIncremental` in `overpass_merge.go` as the orchestrator that replaces `convertPBFsToBZ2` in `runOverpassStart`. Adds orphan cache cleanup, cache-validity-based skipping, and per-region BZ2 merging.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass_merge.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass_merge_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `convertRegionsIncremental` correctly orchestrates per-region caching and merging | PASS |
| Cached regions are skipped on subsequent runs | PASS |
| Orphan cache files are cleaned up when regions are removed | PASS |
| Final `merged.osm.bz2` is a valid BZ2 file with correct merged/deduplicated OSM XML | PASS |
| `runOverpassStart` uses the new incremental pipeline | PASS |
| Region-change logic preserves the cache directory | PASS |
| Tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test ./...  # github.com/yardbirdsax/twisty passes; route package fails on external TLS (unrelated)
```

---

## Final Verdict

**APPROVED**

Both issues from the previous review are resolved: `cleanOrphanCacheFiles` correctly accepts `stderr io.Writer` as a third parameter, and `recordingConvertProgress` is used in `TestConvertRegionsIncremental_ProgressReporting` with an assertion on `setFilesCalls`.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **NEEDS REVISION**: One or more issues found. Address all MUST FIX items before re-review.
