# Task 003: Design Incremental Caching Strategy

## Summary

Based on the profiling findings from Task 002, design the per-region caching strategy that will enable incremental region addition without re-processing existing regions. Document the design as a technical specification.

## Dependencies

Task 002

## Detailed Directions

### 1. Review Profiling Findings

- Read `_specs/0008_better_overpass_caching/profiling_results.md` from Task 002.
- Identify the dominant bottleneck stage.

### 2. Select Caching Strategy

Based on the bottleneck, choose from:

**If BZ2 compression dominates (most likely):**
- Cache per-region BZ2 XML files: `{dataDir}/cache/{region_filename}.osm.bz2`
- On region addition, only convert the new region's PBF to BZ2.
- Final merge: decompress all per-region BZ2s, merge-dedup, re-compress to `merged.osm.bz2`.
- Net savings: avoids re-running PBF decode for existing regions (some savings), but still re-compresses. Consider whether caching uncompressed XML (`.osm`) is better.

**If PBF decoding dominates:**
- Cache per-region decoded XML: `{dataDir}/cache/{region_filename}.osm`
- On addition, only decode the new PBF.
- Final merge: merge-dedup all cached XMLs, compress to BZ2.
- Avoids the expensive decode for existing regions.

**If both PBF decode + BZ2 compress dominate together:**
- Cache per-region sorted, deduplicated-within-region XML (uncompressed or lightly compressed with gzip for faster re-read).
- Final merge: fast re-read of cached files, N-way merge-dedup across regions, BZ2 compress once.
- This avoids PBF decode for existing regions; cross-region dedup must still happen.

### 3. Design the Cache Invalidation Logic

- Define when a per-region cache file is valid (same PBF file exists, same size/mtime as when cached).
- Define cache file naming convention (derive from `pbfFilename` pattern).
- Define what happens on `twisty overpass clean` (delete entire cache dir).

### 4. Design the Modified Pipeline Flow

Document the new `runOverpassStart` logic when regions change:
1. For each region, check if cache file exists and is valid.
2. For regions without a valid cache, run single-region conversion (PBF -> cache format).
3. Merge all per-region cache files into `merged.osm.bz2`.
4. Proceed with existing DB import logic.

### 5. Write the Design Document

- Create `_specs/0008_better_overpass_caching/incremental_design.md` with:
  - Chosen strategy and rationale (linked to profiling data)
  - Cache file format and naming
  - Cache invalidation rules
  - Modified pipeline flow (pseudocode)
  - Correctness argument (dedup is preserved)
  - Estimated speedup (back-of-envelope from profiling percentages)

## Acceptance Criteria

- [ ] Design document exists and is complete
- [ ] Strategy is justified by profiling data (references specific percentages)
- [ ] Cache invalidation logic is specified
- [ ] Deduplication correctness is explicitly addressed
- [ ] Modified pipeline flow is clear enough to implement directly

## Notes

- The key correctness constraint: objects shared across region borders must still be deduplicated in the final merged output. Per-region caching cannot skip cross-region dedup.
- Per-region internal dedup (same object appearing in multiple blocks of one PBF) is handled by the PBF scanner already emitting sorted objects — verify this assumption.
- The design should account for the fact that `MergeScanners` expects `ObjectScanner` inputs — any cached format needs a scanner implementation.
