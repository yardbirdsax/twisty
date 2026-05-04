# Task 006: Implement Incremental Merge Pipeline

## Summary

Replace the monolithic `convertPBFsToBZ2` call with an incremental pipeline that: (1) converts only new/stale regions to cache, (2) merges all cached regions into the final `merged.osm.bz2`. This is the core optimization that avoids re-processing existing regions.

## Dependencies

Task 004, Task 005

## Detailed Directions

### 1. Implement the New Pipeline Orchestrator

Create a new function (or refactor the existing flow in `runOverpassStart`):

```go
func convertRegionsIncremental(pbfPaths []string, cacheDir string, mergedBZ2Path string, progress osmconv.ConvertProgress) error
```

Logic:
1. For each PBF path, derive the corresponding cache path.
2. Call `isCacheValid(pbfPath, cachePath)` for each region.
3. For regions with invalid/missing cache, call `convertSingleRegionToCache(pbfPath, cachePath, progress)`.
4. Once all caches are valid, open all cache files and create scanners.
5. Use `osmconv.MergeScanners(scanners...)` to merge-dedup across regions.
6. Write the merged output through BZ2 compression to `mergedBZ2Path`.
7. Close all files.

### 2. Modify runOverpassStart to Use the New Pipeline

- Replace the existing call to `convertPBFsToBZ2` with `convertRegionsIncremental`.
- Ensure the `cacheDir` (`{dataDir}/cache/`) is created if it doesn't exist.
- Remove the old "delete merged.osm.bz2 on region change" logic — the incremental pipeline handles this naturally (new regions get cached, then everything is re-merged).
- Keep the "delete db/ on region change" logic — the Overpass DB still needs to be re-imported when the merged file changes.

### 3. Update Region Change Detection

- The existing logic deletes `merged.osm.bz2` when regions change. Now:
  - Still delete `merged.osm.bz2` (forces re-merge from caches).
  - Do NOT delete per-region cache files for existing regions.
  - Do NOT delete `db/` yet — only delete it if `merged.osm.bz2` had to be regenerated.

### 4. Integrate Progress Reporting

- The per-region conversion steps should report progress (bytes read from PBF).
- The final merge step should report progress (objects written).
- Adapt the existing `ConvertProgress` interface or use it for both stages.

### 5. Write Tests

- **Incremental test:** Convert 2 regions, then add a 3rd. Verify only the 3rd region's PBF is re-processed (check that cache files for regions 1 and 2 are not modified).
- **Correctness test:** Compare the output of the incremental pipeline to the old monolithic pipeline for the same input. The `merged.osm.bz2` should be byte-for-byte identical (or semantically equivalent — same objects in same order).
- **Full re-conversion test:** With no caches present, the incremental pipeline produces the same output as the old pipeline.

## Acceptance Criteria

- [ ] Adding a new region only converts the new region's PBF (existing caches are reused)
- [ ] Final merged output is correct (deduplicated, sorted, all objects present)
- [ ] Output is identical to the old monolithic pipeline for the same inputs
- [ ] Progress reporting works for both per-region conversion and final merge
- [ ] Tests pass for incremental addition, full conversion, and correctness comparison

## Notes

- The final merge (reading all caches + merge-dedup + BZ2 compress) still runs every time the region set changes. The savings come from skipping PBF decode for existing regions. If BZ2 compression is the dominant cost, the savings may be less dramatic — but PBF decode is typically significant too.
- If profiling shows BZ2 compression dominates overwhelmingly, consider whether caching in BZ2 format and using a fast-path for the single-region case makes sense. But the merge step always needs decompressed input for dedup, so uncompressed/gzip cache is likely better.
- The `MergeScanners` function already handles N-way sorted merge with dedup — reuse it directly.
