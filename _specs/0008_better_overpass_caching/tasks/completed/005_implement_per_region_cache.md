# Task 005: Implement Per-Region Cache Conversion and Validity Checking

## Summary

Implement the ability to convert a single region's PBF file into a gzip-compressed XML cache file, and to check whether an existing cache file is still valid relative to its source PBF.

## Dependencies

Task 004

## Detailed Directions

### 1. Implement Cache Validity Checking

Create a function (in `overpass.go` or a helper file):

```go
func isCacheValid(pbfPath, cachePath, metaPath string) bool
```

Logic:
1. Check that `cachePath` exists.
2. Check that `metaPath` exists and is parseable as JSON.
3. Stat the PBF file at `pbfPath`.
4. Compare PBF size and mtime against the values stored in the sidecar metadata.
5. Return true only if all checks pass.

Sidecar metadata format (`*.meta.json`):
```json
{
  "pbf_size": 58923456,
  "pbf_mtime": "2026-04-28T12:00:00Z"
}
```

### 2. Implement Single-Region Conversion

Create a function:

```go
func convertSingleRegionToCache(pbfPath, cachePath, metaPath string, progress osmconv.ConvertProgress) error
```

Steps:
1. Open the PBF file, create a `PBFScanner`.
2. Create a temp file in the same directory as `cachePath`.
3. Wrap the temp file in `gzip.NewWriter`, then `osmconv.NewXMLWriter`.
4. Run `osmconv.Convert` with the single PBF scanner and the gzip XML writer.
5. Close the gzip writer and the temp file.
6. Stat the PBF file and write the sidecar `metaPath` with size + mtime.
7. Atomically rename the temp file to `cachePath`.

The atomic write (temp + rename) ensures that a crash mid-conversion doesn't leave a corrupt cache file.

### 3. Implement Cache Filename Helper

```go
func cacheFilename(region string) string
```

Derives the cache filename from the region path. Same logic as `pbfFilename()` but with `.osm.gz` extension instead of `.osm.pbf`.

### 4. Write Tests

- **Cache validity**: Test all failure modes — missing cache, missing metadata, PBF size mismatch, PBF mtime mismatch, and the happy path (all match).
- **Single-region conversion**: Given a small PBF, produces a valid `.osm.gz` file that can be read back by `GzipXMLScanner` (from Task 004). Verify object count matches direct PBF scanning.
- **Atomic write**: Verify that if conversion fails mid-way, no cache file is left behind (only the temp file, which is cleaned up).
- **Sidecar metadata**: Verify the metadata JSON is written correctly and round-trips through `isCacheValid`.

## Acceptance Criteria

- [ ] `isCacheValid` correctly identifies valid and stale caches
- [ ] `convertSingleRegionToCache` produces a valid gzip XML cache from a PBF
- [ ] Cache files are written atomically (temp file + rename)
- [ ] Sidecar metadata captures PBF size and mtime
- [ ] Round-trip: PBF -> cache -> `GzipXMLScanner` produces same objects as `PBFScanner` directly
- [ ] Tests pass

## Notes

- The `progress` parameter should report bytes read from the PBF for progress tracking, consistent with existing `ConvertProgress` usage.
- Use `os.CreateTemp` in the cache directory for the temp file, then `os.Rename` to the final path.
- Mtime comparison should use `time.Time.Equal()` or truncate to second precision to avoid filesystem-dependent sub-second mtime differences.

---

# Task 005 Review: Implement Per-Region Cache Conversion and Validity Checking

**Reviewer:** Josh Feierman
**Date:** 2026-05-03
**Verdict:** APPROVED

---

## Summary

This task implements `cacheFilename`, `isCacheValid`, and `convertSingleRegionToCache`, along with a test helper (`BuildMinimalPBFBytes`) for generating synthetic PBF data. All revision items from the prior review have been addressed.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass_cache.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/overpass_cache_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/osmconv/testpbf.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/osmconv/testhelpers_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `isCacheValid` correctly identifies valid and stale caches | PASS |
| `convertSingleRegionToCache` produces a valid gzip XML cache from a PBF | PASS |
| Cache files are written atomically (temp file + rename) | PASS |
| Sidecar metadata captures PBF size and mtime | PASS |
| Round-trip: PBF -> cache -> `GzipXMLScanner` produces same objects as `PBFScanner` directly | PASS |
| Tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
go test github.com/yardbirdsax/twisty -run TestConvert -v   # All PASS
go test github.com/yardbirdsax/twisty -run TestIsCacheValid -v  # All PASS
go test ./...  # All packages pass (one unrelated integration test failure in route package due to TLS cert issue in sandbox)
go vet ./...   # Clean
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. Prior revision items resolved: rename now precedes metadata write (`overpass_cache.go:131`), round-trip test compares objects element-by-element including IDs/coordinates/tags/node-refs (`overpass_cache_test.go:226-269`), and the `BuildMinimalPBFBytes` production-binary concern is acceptably resolved given Go linker dead-stripping of unused symbols.
