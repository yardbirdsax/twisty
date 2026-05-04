# Task 004: Implement Per-Region Conversion

## Summary

Implement the ability to convert a single region's PBF file into a cached intermediate format (per the design from Task 003). This is the building block for incremental caching — each region can be independently converted and its result reused.

## Dependencies

Task 003

## Detailed Directions

### 1. Define Cache File Format and Location

- Based on the design document from Task 003, implement the chosen cache format.
- Most likely: per-region sorted OSM XML files (uncompressed or gzip-compressed for fast re-read).
- Cache files go in `{dataDir}/cache/` with names derived from `pbfFilename()` (e.g., `north-america_us_new-york-latest.osm.gz`).

### 2. Implement Single-Region Conversion Function

In `overpass.go` (or a helper file if cleaner):

```go
func convertSingleRegionToCache(pbfPath string, cachePath string, progress osmconv.ConvertProgress) error
```

- Opens the PBF file and creates a `PBFScanner`.
- Creates the cache output file with the chosen writer (XML writer, possibly wrapped in gzip).
- Runs `osmconv.Convert` with a single scanner and the cache writer.
- This produces a sorted, within-region-deduplicated intermediate file.

### 3. Implement Cache File Scanner

If the cache format is not raw OSM XML (e.g., gzip-compressed), implement an `ObjectScanner` that can read the cached format back:

- In `osmconv/`, add an `XMLScanner` (or `GzipXMLScanner`) that reads the cached XML format and produces `Object` values via the `ObjectScanner` interface.
- The scanner must emit objects in sorted order (which they will be, since they were written sorted).

### 4. Write Tests

- Test single-region conversion: given a small PBF, produces a valid cache file.
- Test round-trip: convert PBF to cache, read cache back with scanner, verify objects match direct PBF scanning (same objects, same order).
- Test that the cache scanner implements `ObjectScanner` correctly (Next/Object/Err contract).

## Acceptance Criteria

- [ ] `convertSingleRegionToCache` converts a PBF to the chosen cache format
- [ ] A corresponding `ObjectScanner` can read the cache file back
- [ ] Round-trip test confirms lossless conversion (same objects in same order)
- [ ] Cache files are written atomically (temp file + rename)
- [ ] Tests pass

## Notes

- The cache format must preserve all object data (IDs, tags, coordinates, way node refs, relation members) — it's the same data, just in a faster-to-read format.
- If the chosen format is uncompressed XML, no new scanner is needed — just use the existing XML parsing. But a dedicated scanner may be faster.
- Keep progress reporting consistent with existing patterns (use `ConvertProgress` interface).
