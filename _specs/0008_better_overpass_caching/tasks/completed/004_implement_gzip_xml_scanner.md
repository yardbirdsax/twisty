# Task 004: Implement GzipXMLScanner

## Summary

Implement a new `ObjectScanner` that reads gzip-compressed OSM XML files and emits `Object` values in sorted order. This scanner is needed to read per-region cache files back during the merge step.

## Dependencies

Task 003

## Detailed Directions

### 1. Implement XMLScanner in `osmconv/`

Create `osmconv/xmlscanner.go` with a streaming XML parser that reads OSM XML (`<node>`, `<way>`, `<relation>` elements) and produces `Object` values via the `ObjectScanner` interface.

The scanner must handle:
- `<node id="..." lat="..." lon="...">` with nested `<tag k="..." v="..."/>` children
- `<way id="...">` with nested `<nd ref="..."/>` and `<tag>` children
- `<relation id="...">` with nested `<member type="..." ref="..." role="..."/>` and `<tag>` children
- The XML header/footer (`<?xml ...>`, `<osm ...>`, `</osm>`) — skip these, only emit objects

The scanner reads from an `io.Reader`, so the gzip decompression layer is composed externally.

### 2. Implement GzipXMLScanner Constructor

Create a constructor `NewGzipXMLScanner(path string) (*GzipXMLScanner, error)` that:
1. Opens the gzip file
2. Wraps it in `gzip.NewReader`
3. Feeds the decompressed stream to `XMLScanner`
4. Returns a type that implements `ObjectScanner` and `io.Closer`

### 3. Write Tests

- **Round-trip test**: Write known objects via `XMLWriter` (gzip-wrapped), read them back via `GzipXMLScanner`, verify all fields match (IDs, tags, coordinates, way node refs, relation members).
- **Sort order test**: Verify that objects come out in the expected order (nodes < ways < relations, ascending ID within type).
- **Empty file test**: An XML file with no objects produces `Next() == false` immediately.
- **Scanner contract test**: Verify `Next()`/`Object()`/`Err()` contract — `Object()` only valid after `Next()` returns true, `Err()` returns nil on clean EOF.

### 4. Verify Compatibility with MergeScanners

Write a test that creates two gzip XML files with overlapping objects, reads both via `GzipXMLScanner`, passes them to `MergeScanners`, and verifies correct dedup behavior. This confirms the scanner integrates correctly with the existing merge pipeline.

## Acceptance Criteria

- [ ] `XMLScanner` correctly parses all OSM object types (nodes, ways, relations) with full field fidelity
- [ ] `GzipXMLScanner` opens a gzip file and returns an `ObjectScanner`
- [ ] Round-trip test passes: `XMLWriter` -> gzip -> `GzipXMLScanner` produces identical objects
- [ ] Scanner integrates with `MergeScanners` (dedup test passes)
- [ ] Tests pass

## Notes

- Use `encoding/xml` for streaming parsing (`xml.Decoder.Token()` loop), not `xml.Unmarshal` on the whole file — the files can be large.
- The XML format is the same format produced by `XMLWriter` — look at `xmlwriter.go` for the exact element structure and attribute names.
- Coordinates are formatted by `XMLWriter` using `formatCoord()` — the scanner must parse these back to `float64` with `strconv.ParseFloat`.
- Object IDs are `int64` — parse with `strconv.ParseInt`.

---

# Task 004 Review: Implement GzipXMLScanner

**Reviewer:** Josh Feierman (via Claude Code)
**Date:** 2026-05-03
**Verdict:** APPROVED

---

## Summary

Implements `XMLScanner` (reads OSM XML from `io.Reader`) and `GzipXMLScanner` (opens a `.osm.gz` file, decompresses, feeds to `XMLScanner`). Both implement `ObjectScanner`. Tests cover round-trip, sort order, empty file, clean-EOF contract, MergeScanners integration, and special character handling.

### Files Reviewed

| File | Status |
|------|--------|
| `osmconv/xmlscanner.go` | Reviewed |
| `osmconv/xmlscanner_test.go` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `XMLScanner` correctly parses all OSM object types with full field fidelity | PASS |
| `GzipXMLScanner` opens a gzip file and returns an `ObjectScanner` | PASS |
| Round-trip test passes | PASS |
| Scanner integrates with `MergeScanners` (dedup test passes) | PASS |
| Tests pass | PASS |

---

## MUST FIX

No blocking issues found.

---

## Verification Commands Run

```bash
make test   # all packages pass, including osmconv (0.666s)
go vet ./osmconv/...  # clean
# make lint fails due to build-cache permission issue unrelated to this code
```

---

## Final Verdict

**APPROVED**

All acceptance criteria met. Tests pass cleanly. No correctness issues found.
