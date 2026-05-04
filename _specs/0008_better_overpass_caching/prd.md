# Product Requirements Document: Better Overpass Caching

## Overview

Twisty's `overpass start` command downloads OpenStreetMap region extracts, merges and converts them into a single BZ2-compressed XML file, and imports that file into a local Overpass API database. The conversion pipeline currently treats the merged output as a monolithic artifact: any change to the region set triggers a full re-conversion of every region from scratch. This project profiles the existing pipeline to identify bottlenecks, then uses those findings to make adding a new region to an existing set as fast as possible.

## Problem Statement

When a user adds a single new region to an existing set of Overpass regions, the entire PBF-to-BZ2 conversion pipeline re-runs for all regions, not just the new one. For a typical 5-region dataset, this means re-decoding, re-merging, and re-compressing hundreds of millions of OSM objects that haven't changed.

1. **Full re-conversion on region addition.** The `.regions` stamp file comparison detects that the region set has grown, deletes `merged.osm.bz2` and the `db/` directory, and forces a complete re-conversion of all PBF files. The individual PBF downloads are correctly cached and skipped, but everything downstream is rebuilt from scratch. For large region sets, this takes a long time and wastes work that was already done.

2. **No visibility into where time is spent.** The conversion pipeline has multiple stages (PBF protobuf decoding, N-way sorted merge with deduplication, XML serialization, BZ2 compression), but there is no profiling or instrumentation to determine which stage dominates wall-clock time. Without this data, optimization efforts are speculative.

## Users

Twisty users who run `twisty overpass start` with multiple US state-level Geofabrik regions and iteratively add new regions over time. These users have a local Docker environment and are willing to trade disk space for faster startup when expanding their region set.

## Design Principles

### 1. Measure Before Optimizing

The pipeline has multiple stages with different computational profiles (binary decoding, heap-based merge, XML serialization, BZ2 compression). Rather than guessing which stage to optimize, we instrument the pipeline first and let profiling data drive the design of Phase 2.

### 2. Minimize Redundant Work

When the region set changes, only the delta should require full processing. Previously converted or merged data should be reusable to the maximum extent possible, constrained by the requirement that Overpass ingests a single merged file.

### 3. Correctness Over Speed

Geofabrik regional extracts overlap at borders -- shared nodes and ways appear in multiple PBF files. Any merge strategy must continue to deduplicate these shared objects. Producing duplicate objects in the Overpass database would cause incorrect query results.

### 4. Disk Space Is Cheap, Time Is Not

Users are willing to accept roughly double the current disk usage if it means adding a new region avoids re-processing all existing regions.

## Feature 1: CPU Profiling for the Conversion Pipeline

### Behavioral / Functional Goal

Give developers visibility into where wall-clock time is spent during the PBF-to-BZ2 conversion, so that optimization decisions in Phase 2 are data-driven rather than speculative.

### Trigger / Entry Point

The user passes a `--cpuprofile <path>` flag to `twisty overpass start`. When present, the conversion pipeline writes a Go `pprof` CPU profile to the specified file. When absent, behavior is unchanged.

### Core Behavior

When `--cpuprofile` is set:

1. Before the conversion pipeline begins, start CPU profiling via `runtime/pprof`.
2. Run the full pipeline (PBF decode, merge, XML write, BZ2 compress) as normal.
3. After the pipeline completes (or errors), stop profiling and flush the profile to the specified path.
4. Print the profile file path to stderr so the user knows where to find it.

The user can then analyze the profile with `go tool pprof <path>` to see a flame graph or top-function breakdown.

### Rules and Constraints

- Profiling only covers the conversion pipeline (`convertPBFsToBZ2`), not PBF downloads or Docker container startup.
- The flag is optional and has no effect on behavior when omitted.
- The profile file is written atomically -- if the conversion fails, a partial profile is still flushed so partial data is available.
- The flag should be implemented cleanly (not behind a build tag or hidden flag) so it can remain in the codebase if useful, but it is not a committed long-term feature.

### Data Model

No persistent data model changes. The profile is a standard Go pprof file written to a user-specified path.

## Feature 2: Optimized Incremental Region Addition

### Behavioral / Functional Goal

When a user adds a new region to an existing set, the pipeline should avoid re-processing regions that have already been converted. Only the new region's data should go through the expensive conversion steps, with a final merge step combining cached and new data.

### Trigger / Entry Point

`twisty overpass start --regions <region list>` where the region list is a superset of the previously loaded regions (i.e., one or more new regions added to the existing set).

### Core Behavior

The specific optimization strategy depends on Phase 1 profiling results. The design space includes:

**If PBF decoding is the bottleneck:**
- Cache per-region converted data in an intermediate format that is fast to re-read (e.g., per-region BZ2 XML files, or a fast binary format).
- When adding a region, only convert the new region's PBF. Then merge all per-region cached files into the final `merged.osm.bz2`.

**If BZ2 compression is the bottleneck:**
- Use `osmium merge` (a C++ tool) to merge PBF files directly into a single PBF, then convert once to BZ2.
- Or explore faster compression alternatives if Overpass accepts them.

**If the merge/dedup step is the bottleneck:**
- Optimize the N-way merge (e.g., larger buffers, parallel decompression of per-region files).

**In all cases:**
- The final output must be a single `merged.osm.bz2` file (Overpass container requirement).
- Objects shared across region borders must be deduplicated.
- Existing PBF download caching is preserved as-is.

### Rules and Constraints

- The Overpass container requires a single planet file at `OVERPASS_PLANET_URL`. Multiple input files are not supported.
- The N-way sorted merge must deduplicate objects with the same (Type, ID) to handle overlapping Geofabrik region extracts.
- Adding a new region may still require re-importing the Overpass database (the container does not support incremental DB updates). The optimization target is the host-side merge/conversion time, not the container import time.
- Per-region cached artifacts are stored in the existing `{dataDir}` directory structure.
- Region removal does not need to be optimized -- users can run `twisty overpass clean` for that case.

### Data Model

Depends on Phase 2 design. Likely additions to the data directory:

```
{dataDir}/
  .regions              # existing stamp file
  pbf/                  # existing per-region PBF downloads
  cache/                # new: per-region intermediate files
    {region}.osm.bz2    # or other format TBD by profiling
  merged.osm.bz2        # existing final merged output
  db/                   # existing Overpass database
```

## Technical Direction

### Profiling Infrastructure

Use Go's standard `runtime/pprof` package. The `--cpuprofile` flag is added to the `overpass start` command. Profiling start/stop is scoped to the `convertPBFsToBZ2` call site in `runOverpassStart`, not pushed down into the `osmconv` package, to keep the instrumentation at the integration layer.

### Conversion Pipeline Optimization

The specific optimization is deferred to Phase 2, informed by profiling. The current pipeline architecture (scanner interface, merge scanner, writer interface) is well-factored for introducing per-region caching -- each region already has its own `ObjectScanner`, and the merge layer is cleanly separated from I/O.

### Integration with Existing System

The profiling flag is a small addition to the existing `overpass start` command. Phase 2 changes are localized to `runOverpassStart` in `overpass.go` and potentially the `osmconv` package, with no impact on other commands.

New code lives in:
- `overpass.go` -- `--cpuprofile` flag, profiling start/stop, per-region cache logic
- `osmconv/` -- potential new scanner/writer implementations for cached intermediate format

## Phasing

### Phase 1: Profile the Conversion Pipeline

**Hypothesis:** One or two stages of the conversion pipeline dominate wall-clock time, and identifying them will reveal a clear optimization target.

**Scope:**
- Add `--cpuprofile` flag to `twisty overpass start`
- Run profiling on a representative multi-region dataset (5 US state-level regions)
- Analyze results to identify the dominant cost center(s)

**Success measurement:**
- Profile clearly identifies which stage(s) account for >50% of conversion time
- Results are reproducible across multiple runs

**Exit criteria for Phase 2:** Profiling data is collected, analyzed, and the dominant bottleneck is identified with enough confidence to choose an optimization strategy.

### Phase 2: Implement Incremental Region Addition

**Hypothesis:** By caching intermediate conversion artifacts per-region (in a format informed by Phase 1 findings), adding a new region can avoid re-processing existing regions' data through the identified bottleneck stage.

**Scope:**
- Implement per-region caching strategy chosen based on Phase 1 data
- Modify the region-change detection logic to preserve and reuse cached data
- Ensure deduplication correctness is maintained
- Update stamp file logic to track per-region cache validity

**Success measurement:**
- Adding a new region to a 4-region set takes less than 50% of the time compared to a full 5-region re-conversion
- Existing regions' cached data is reused without re-processing
- Merged output is byte-for-byte identical (or semantically equivalent) to a full re-conversion

**Exit criteria for Phase 2:** N/A (final phase).

## Non-Goals

- Optimizing region removal (users can `twisty overpass clean` and re-run)
- Incremental Overpass database updates (the container does not support this)
- Freshness checking of downloaded PBF files (Geofabrik update detection)
- Reducing the Overpass container's own import time
- Changing the Overpass container image or its initialization workflow

## Success Criteria

1. Phase 1 produces actionable profiling data that identifies the conversion bottleneck
2. Phase 2 reduces wall-clock time for adding a new region by at least 50% compared to full re-conversion
3. Merged output correctness is maintained (deduplication, sorting)
4. No regression in the happy path (same regions, no changes) performance

## Future Considerations

- **PBF freshness checking:** Geofabrik publishes new extracts regularly. A future enhancement could check `Last-Modified` headers and re-download stale PBFs, with corresponding cache invalidation of per-region intermediate files.
- **Region removal optimization:** If users frequently remove regions, the per-region cache could support efficient re-merge from a subset.
- **Parallel conversion:** Per-region caching naturally enables parallel conversion of multiple new regions, since each region's PBF can be processed independently before the final merge.
- **Alternative compression:** If BZ2 compression is the bottleneck, future work could explore whether Overpass accepts other formats (e.g., gzip) or whether a faster BZ2 implementation exists.
