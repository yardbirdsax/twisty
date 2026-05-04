# Task 002: Run Profiling and Document Findings

## Summary

Use the `--cpuprofile` flag from Task 001 on a representative multi-region dataset to identify the dominant bottleneck in the conversion pipeline. Document findings to inform the Phase 2 optimization strategy.

## Dependencies

Task 001

## Detailed Directions

### 1. Prepare a Representative Dataset

- Choose 3-5 US state-level Geofabrik regions (e.g., `north-america/us/delaware`, `north-america/us/rhode-island`, `north-america/us/connecticut`, `north-america/us/new-hampshire`, `north-america/us/vermont`) — smaller states to keep the profiling run manageable.
- Ensure PBFs are already downloaded (run once without profiling if needed).

### 2. Run the Profiled Conversion

- Run `twisty overpass start --cpuprofile /tmp/overpass_conv.prof --regions <regions>` with a clean merged state (delete `merged.osm.bz2` first to force re-conversion).
- Run at least 2 times to confirm reproducibility.

### 3. Analyze the Profile

- Use `go tool pprof -top /tmp/overpass_conv.prof` to get top functions by CPU time.
- Use `go tool pprof -web /tmp/overpass_conv.prof` (or `-svg`) for a flame graph.
- Categorize time into the pipeline stages:
  - **PBF decoding** (functions in `pbfscanner.go`, protobuf unmarshal, zlib decompress)
  - **Merge/dedup** (heap operations in `merge.go`)
  - **XML serialization** (functions in `xmlwriter.go`, `bufio`, `fmt`)
  - **BZ2 compression** (`dsnet/compress/bzip2` writer)

### 4. Document Findings

- Create a file `_specs/0008_better_overpass_caching/profiling_results.md` with:
  - Dataset description (regions, PBF sizes, total objects)
  - Top 10 functions by cumulative CPU time
  - Percentage breakdown by pipeline stage
  - Identification of the dominant bottleneck (>50% threshold)
  - Recommended optimization strategy for Phase 2

## Acceptance Criteria

- [ ] Profiling run completed successfully on 3+ regions
- [ ] Results are reproducible across multiple runs (dominant stage is consistent)
- [ ] Findings document clearly identifies which stage accounts for >50% of conversion time
- [ ] Document includes a recommended optimization strategy with rationale

## Notes

- This is a manual analysis task — the "deliverable" is the findings document, not code.
- The findings document will be the input for Task 003's design decisions.
- If no single stage dominates >50%, document the split and recommend a combined strategy.
- Expected outcome based on pipeline architecture: BZ2 compression is likely dominant (bzip2 is notoriously CPU-intensive), followed by PBF decoding (protobuf + zlib).
