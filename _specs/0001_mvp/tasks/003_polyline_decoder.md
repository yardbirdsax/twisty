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
