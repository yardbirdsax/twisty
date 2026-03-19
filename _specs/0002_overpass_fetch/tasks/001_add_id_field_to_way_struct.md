# Task 001: Add ID Field to Way and overpassElement Structs

## Summary

Add an `ID int64` field to the `Way` and `overpassElement` structs in `quality/overpass.go` so that OSM way IDs are preserved through the fetch pipeline. This is the foundational change that enables deduplication in the tiled fetch system without breaking the existing route-quality pipeline.

## Dependencies

None — this is the foundational task.

## Detailed Directions

### 1. Add ID field to `overpassElement` struct

- In `quality/overpass.go`, add `ID int64 \`json:"id"\`` to the `overpassElement` struct (currently at line ~60).
- This field is already present in Overpass JSON responses when using `out geom`; it was simply not being captured.

### 2. Add ID field to `Way` struct

- In `quality/overpass.go`, add `ID int64` to the `Way` struct (currently at line ~17).
- This is a public field since downstream consumers (tile merge, deduplication) need access.

### 3. Propagate ID in `fetchWaysFromURL`

- In the loop inside `fetchWaysFromURL` (line ~120) where `overpassElement` is converted to `Way`, copy the `ID` field:
  ```go
  ways = append(ways, Way{
      ID:       el.ID,
      Tags:     el.Tags,
      Geometry: coords,
  })
  ```

### 4. Verify existing tests still pass

- Run `go test ./quality/...` to ensure the new field doesn't break anything.
- The existing `TestFetchWaysSuccess` httptest mock should still pass since the mock response either already includes `"id"` or JSON unmarshaling will default it to 0 for missing fields.

### 5. Add a test for ID parsing

- In `quality/overpass_test.go`, add a test `TestFetchWaysPreservesID` that:
  - Sets up an httptest server returning a response with a known `"id"` field on the way element.
  - Calls `fetchWaysFromURL` and asserts the returned `Way.ID` matches the expected value.

## Acceptance Criteria

- [ ] `Way` struct has an `ID int64` field
- [ ] `overpassElement` struct has an `ID int64 \`json:"id"\`` field
- [ ] `fetchWaysFromURL` copies the ID from element to Way
- [ ] All existing tests in `quality/` pass without modification
- [ ] New test verifies ID is correctly parsed from Overpass JSON response
- [ ] `go vet ./...` and `go build ./...` pass cleanly

## Notes

- The ID field defaults to 0 for any response that doesn't include it, which is safe for the existing route-quality pipeline (it doesn't use the ID).
- This is intentionally a minimal change to reduce risk to the existing pipeline.
