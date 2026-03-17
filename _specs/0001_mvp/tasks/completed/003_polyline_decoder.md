# Task 003: Google Encoded Polyline Decoder

## Summary

Implement the Google Encoded Polyline decoder (precision 5) in the `geo` package, converting OSRM route geometry strings into `[]Coord` slices.

## Dependencies

Task 002 (requires `geo.Coord`)

## Detailed Directions

### 1. Implement DecodePolyline

Add the following function to `geo/geo.go`:

```go
// DecodePolyline decodes a Google Encoded Polyline (precision 5) string
// into a slice of Coord.
func DecodePolyline(encoded string) []Coord
```

Algorithm (delta-encoded 5-bit variable-length quantities):

1. Maintain integer accumulators `latAcc` and `lonAcc`, both starting at 0.
2. Maintain a byte index `i` into the string.
3. Loop until all bytes are consumed, alternating between decoding lat and lon:
   a. Initialize `result = 0`, `shift = 0`.
   b. Loop: read byte `b = encoded[i] - 63`. Increment `i`.
      - OR `(b & 0x1F) << shift` into `result`.
      - Increment `shift` by 5.
      - If `b < 0x20`, break (this was the last chunk).
   c. If `result & 1` is set (negative flag): `result = ^result`.
   d. Delta = `result >> 1`.
   e. Add delta to the appropriate accumulator (`latAcc` or `lonAcc`).
4. Append `Coord{Lat: float64(latAcc)/1e5, Lon: float64(lonAcc)/1e5}` to the result slice.

### 2. Write Unit Tests

Create or extend `geo/geo_test.go` with tests for `DecodePolyline`:

- Empty string → empty slice.
- Known short encoded string → verify exact decoded coordinates.
  - Example: `"_p~iF~ps|U_ulLnnqC_mqNvxq`@"` decodes to:
    - `(38.5, -120.2)`, `(40.7, -120.95)`, `(43.252, -126.453)`
    - (This is the canonical Google example.)
- Single point string → one-element slice.

## Acceptance Criteria

- [ ] `go test ./geo/` passes including polyline tests
- [ ] The canonical Google example decodes to the three correct coordinate pairs (within float precision)
- [ ] Empty string returns an empty (non-nil) slice
- [ ] Function does not panic on malformed input (short strings, truncated sequences)

## Notes

- The decoder must handle negative deltas correctly via the one's complement check on the lowest bit.
- OSRM polylines use precision 5 (divide by 1e5), which is standard.
- The function will be called from `route/osrm.go` in Task 005.

---
# Task 003 Review: Google Encoded Polyline Decoder

**Reviewer:** Claude Sonnet 4.6
**Date:** 2026-03-16
**Verdict:** APPROVED

---

## Summary

Implements `DecodePolyline(encoded string) []Coord` in the `geo` package using the standard Google Encoded Polyline algorithm (precision 5). Tests cover empty string, canonical Google example, single point, malformed input, and truncated-after-lat input.

### Files Reviewed

| File | Status |
|------|--------|
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/geo.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/geo/geo_test.go` | Reviewed |
| `/Users/joshuafeierman/repos/yardbirdsax/twisty/Makefile` | Reviewed |

### Acceptance Criteria Verification

| Criterion | Result |
|-----------|--------|
| `go test ./geo/` passes including polyline tests | PASS |
| Canonical Google example decodes to three correct coordinate pairs | PASS |
| Empty string returns an empty (non-nil) slice | PASS |
| Function does not panic on malformed input | PASS |

---

## MUST FIX

No blocking issues found.

---

## SHOULD FIX

No additional suggestions.

---

## Good Practices Observed

1. **Non-nil empty slice:** `make([]Coord, 0)` satisfies the non-nil empty slice requirement without special-casing the empty input.
2. **Truncated-input guard:** The `lonDecoded` flag prevents appending a partial coord when input is truncated between lat and lon chunks.
3. **Extra test coverage:** `TestDecodePolyline_TruncatedAfterLat` explicitly guards the truncated-between-lat-and-lon edge case beyond the spec minimum.

---

## Verification Commands Run

```bash
go test ./geo/ -v   # all 9 tests PASS
go vet ./...        # no issues
make test           # geo PASS (root package has no test files; build cache issue in sandbox only, not a code defect)
make lint           # no issues
```

---

## Final Verdict

**APPROVED**

All acceptance criteria pass, `go vet` is clean, and the implementation correctly handles all specified edge cases including truncated input and negative delta decoding.

---

## Verdict Definitions

- **APPROVED**: All acceptance criteria met, no issues found. Ready to merge.
- **APPROVED WITH CHANGES**: All acceptance criteria met, minor issues found. Can merge after addressing SHOULD FIX items, or merge as-is with follow-up.
- **NEEDS REVISION**: Acceptance criteria not met or critical issues found. Must address MUST FIX items before re-review.
