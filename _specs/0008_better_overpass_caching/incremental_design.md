# Incremental Caching Design: Per-Region Cache with Parallel BZ2 Compression

## Chosen Strategy and Rationale

### Profiling Summary

The conversion pipeline is **compression-bound**. BZ2 compression accounts for **87.6%** of all CPU time. The remaining stages are negligible:

| Stage | CPU % |
|-------|-------|
| BZ2 compression | 87.6% |
| XML serialization | 4.7% |
| Merge/dedup | 3.7% |
| PBF decoding | 3.4% |

### Why Per-Region Caching Alone Is Insufficient

A naive per-region cache (e.g., cache each region's decoded XML) would only skip PBF decoding for existing regions — saving ~3.4% per cached region. The final merge still requires BZ2 compression of the entire output, which dominates. Even with all 4 of 5 regions cached, the pipeline would still spend ~87% of its time in BZ2 compression, yielding negligible speedup.

### Strategy: Per-Region BZ2 Cache + Parallel BZ2 Block Compression

The strategy combines two optimizations that together target the bottleneck:

1. **Per-region sorted XML cache (gzip-compressed)**: Cache each region's PBF-decoded, sorted OSM objects as gzip-compressed XML. When a new region is added, only the new region's PBF needs decoding. Gzip is used (not BZ2) because re-reading the cache must be fast — gzip decompression is ~10x faster than BZ2 decompression.

2. **Parallel BZ2 block compression**: BZ2 is a block-based compressor. Each 900KB block is compressed independently via the Burrows-Wheeler Transform. A parallel writer dispatches blocks to a pool of goroutines (one per CPU core), compressing multiple blocks concurrently. This targets the 87.6% bottleneck directly and should yield near-linear speedup with core count.

### Expected Speedup

On a machine with N cores:

- **Per-region caching** saves PBF decode time for cached regions: ~3.4% * (cached/total) regions. For adding 1 region to 4 cached: saves ~2.7%.
- **Parallel BZ2 compression** reduces the 87.6% BZ2 cost by roughly 1/N. On a 4-core machine: ~87.6% / 4 = ~22% of original, saving ~65%.
- **Combined**: the 12-minute benchmark should drop to ~2-3 minutes on 4+ cores.

The 50% speedup target from the PRD is achievable primarily through parallel BZ2 compression. Per-region caching provides incremental savings and — critically — enables the workflow where adding a region doesn't re-process existing regions through PBF decode.

## Cache File Format and Naming

### Format

Per-region cache files are **gzip-compressed OSM XML** (`.osm.gz`).

Rationale:
- Gzip decompression is ~10x faster than BZ2, so re-reading cached regions for the merge step is cheap.
- The existing `XMLWriter` produces Overpass-compatible sorted XML — reusing it for cache output means no new serialization code.
- A corresponding `XMLScanner` (new) reads gzip XML back as an `ObjectScanner`, plugging directly into `MergeScanners`.
- Gzip compression ratio is adequate for caching (not as good as BZ2, but disk space is cheap per the PRD's design principles).

### Naming Convention

```
{dataDir}/cache/{pbfFilename_without_extension}.osm.gz
```

Example: region `north-america/us/delaware` with PBF filename `north-america_us_delaware-latest.osm.pbf` produces cache file `north-america_us_delaware-latest.osm.gz`.

### Cache Metadata

Each cache file has a sidecar `.meta.json` file containing the PBF's size and modification time at the time of caching:

```json
{
  "pbf_size": 58923456,
  "pbf_mtime": "2026-04-28T12:00:00Z"
}
```

This enables cache validity checking without content hashing.

## Cache Invalidation Rules

A per-region cache file is **valid** if and only if:

1. The cache file `{dataDir}/cache/{name}.osm.gz` exists.
2. The sidecar `{dataDir}/cache/{name}.meta.json` exists and is parseable.
3. The corresponding PBF file `{dataDir}/pbf/{name}.osm.pbf` exists.
4. The PBF file's current size and mtime match the values in the sidecar metadata.

If any condition fails, the cache is stale and the region must be re-converted from PBF.

### Invalidation Triggers

- **PBF re-downloaded**: Size or mtime changes, invalidating the cache.
- **Cache file deleted**: User ran `twisty overpass clean` or manually deleted.
- **Region removed**: Stale cache files for removed regions are cleaned up during the merge step (orphan cleanup).

## Modified Pipeline Flow

### Current Flow (before)

```
for each region:
    download PBF if missing
if merged.osm.bz2 missing:
    open all PBFs as PBFScanners
    MergeScanners -> XMLWriter -> bzip2.Writer -> merged.osm.bz2
```

### New Flow (after)

```
for each region:
    download PBF if missing

create {dataDir}/cache/ directory

for each region (parallelizable in future):
    if cache valid (PBF size+mtime match sidecar):
        skip
    else:
        convert PBF -> cache/{name}.osm.gz
        write sidecar cache/{name}.meta.json

clean orphan cache files (regions no longer in set)

if merged.osm.bz2 missing:
    open all cache files as GzipXMLScanners
    MergeScanners -> XMLWriter -> ParallelBZ2Writer -> merged.osm.bz2
```

### Pseudocode

```go
func convertRegionsIncremental(allRegions []string, dataDir string, progress ConvertProgress) error {
    cacheDir := filepath.Join(dataDir, "cache")
    os.MkdirAll(cacheDir, 0o755)

    // Phase 1: Ensure per-region caches are up to date.
    for _, region := range allRegions {
        pbfPath := filepath.Join(dataDir, "pbf", pbfFilename(region))
        cacheName := cacheFilename(region) // e.g., "north-america_us_delaware-latest.osm.gz"
        cachePath := filepath.Join(cacheDir, cacheName)
        metaPath := cachePath + ".meta.json"

        if isCacheValid(pbfPath, cachePath, metaPath) {
            // Cache hit — skip conversion.
            continue
        }

        // Cache miss — convert this region's PBF to gzip XML cache.
        if err := convertSingleRegionToCache(pbfPath, cachePath, metaPath, progress); err != nil {
            return fmt.Errorf("caching %s: %w", region, err)
        }
    }

    // Phase 2: Clean orphan cache files for removed regions.
    cleanOrphanCacheFiles(cacheDir, allRegions)

    // Phase 3: Merge all cached regions into final BZ2.
    mergedBZ2 := filepath.Join(dataDir, "merged.osm.bz2")
    var cachePaths []string
    for _, region := range allRegions {
        cachePaths = append(cachePaths, filepath.Join(cacheDir, cacheFilename(region)))
    }

    return mergeRegionCachesToBZ2(cachePaths, mergedBZ2, progress)
}

func mergeRegionCachesToBZ2(cachePaths []string, bz2Path string, progress ConvertProgress) error {
    // Open each cache file as a GzipXMLScanner (implements ObjectScanner).
    scanners := make([]ObjectScanner, len(cachePaths))
    for i, p := range cachePaths {
        scanners[i] = NewGzipXMLScanner(p)
    }

    // Write merged output through parallel BZ2 writer.
    outFile := os.Create(bz2Path)
    pbz2 := NewParallelBZ2Writer(outFile, runtime.GOMAXPROCS(0))
    xmlw := NewXMLWriter(pbz2)

    Convert(ConvertOptions{Scanners: scanners, Writer: xmlw, Progress: progress})

    pbz2.Close()
    outFile.Close()
}
```

## Correctness Argument

### Deduplication Is Preserved

The correctness constraint is: objects shared across region borders (same Type+ID in multiple PBFs) must appear exactly once in the merged output.

This property is maintained because:

1. **Per-region caches are sorted.** Each cache is produced by `PBFScanner` (which emits sorted objects) through `Convert` with a single scanner. The output XML preserves sort order (nodes < ways < relations, then by ID within type).

2. **`MergeScanners` deduplicates.** The merge step reads all per-region caches through `GzipXMLScanner` instances (which preserve sort order) and feeds them to `MergeScanners`, which performs N-way sorted merge with consecutive duplicate elimination. This is identical to the current pipeline's dedup behavior.

3. **No data loss in cache format.** The gzip XML cache preserves all object fields (IDs, tags, coordinates, way node refs, relation members) — it is the same XML format written by `XMLWriter`, just gzip-compressed instead of BZ2-compressed.

The merged output is semantically identical regardless of whether inputs are PBF files or gzip XML caches. The only difference is the compression wrapper on the intermediate files.

### Parallel BZ2 Preserves Output

Parallel BZ2 compression produces a valid BZ2 stream. Each block is independently compressed (this is how BZ2 works), and the parallel writer reassembles blocks in input order. The output is a compliant `.bz2` file that any BZ2 decompressor can read. The decompressed content is byte-for-byte identical to what a serial BZ2 writer would produce for the same block boundaries.

Note: the compressed bytes may differ from serial BZ2 (block boundaries may land at different positions), but the decompressed content is identical. Correctness verification should compare decompressed output, not compressed bytes.

## New Components Required

### 1. `GzipXMLScanner` (osmconv package)

An `ObjectScanner` that reads gzip-compressed OSM XML and emits `Object` values in sorted order. This is a streaming XML parser that handles `<node>`, `<way>`, and `<relation>` elements.

### 2. `ParallelBZ2Writer` (osmconv package)

An `io.Writer` that splits input into BZ2-sized blocks and compresses them in parallel using a goroutine pool. Blocks are reassembled in order for the output stream. Implements `io.WriteCloser`.

### 3. `convertSingleRegionToCache` (overpass.go or helper)

Converts a single PBF file to a gzip-compressed XML cache file. Uses atomic write (temp file + rename).

### 4. `isCacheValid` (overpass.go or helper)

Checks cache validity by comparing PBF stat info against sidecar metadata.

### 5. `convertRegionsIncremental` (overpass.go)

Orchestrator function replacing `convertPBFsToBZ2` in `runOverpassStart`.

## Data Directory Layout (after)

```
{dataDir}/
  .regions                # existing: comma-separated region list
  pbf/                    # existing: downloaded PBF files
    north-america_us_delaware-latest.osm.pbf
    ...
  cache/                  # new: per-region gzip XML caches
    north-america_us_delaware-latest.osm.gz
    north-america_us_delaware-latest.osm.gz.meta.json
    ...
  merged.osm.bz2          # existing: final merged output
  db/                     # existing: Overpass database
```
