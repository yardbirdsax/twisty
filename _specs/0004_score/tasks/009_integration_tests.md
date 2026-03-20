# Task 009: Integration Tests

## Summary

Add end-to-end integration tests that exercise the full stages 5-7 pipeline with realistic synthetic data. These tests validate that the pipeline produces correct, usable KML output and that all stages interact correctly.

## Dependencies

Task 007 or Task 008 — requires the full pipeline to be wired.

## Detailed Directions

### 1. Create Integration Test File

- Create `quality/pipeline_integration_test.go` (or extend `quality/score_integration_test.go` if it exists and is appropriate).

### 2. Build Realistic Test Fixtures

- Create helper functions that generate synthetic `[]ScoredWay` data representing:
  - **A twisty secondary road** ("Mountain Pass Rd"): 15-20 ways with mixed tier 2-4 segments, highway type "secondary"
  - **A straight highway with an interchange** ("Interstate 90"): 10 ways, mostly tier 0 with a few tier 2-3 segments (on-ramps), highway type "motorway"
  - **A road with a long straight gap** ("Route 100"): two clusters of twisty segments separated by >2.5 km of tier-0 segments, highway type "tertiary"
  - **Two disconnected roads with the same name** ("Main Street"): two clusters of ways, geographically far apart
  - **An unnamed road**: ways with no `name` tag

### 3. Test: Full Pipeline Produces Valid KML

- Run `Aggregate` → `ApplyPenalties` → `WriteKML` on the test fixtures.
- Parse the output XML and verify:
  - Document name is "Twisty Roads"
  - Five tier styles present
  - Expected number of folders (road collections)
  - Road names appear correctly
  - Coordinates are in `lon,lat,0` format

### 4. Test: Aggregation Correctness

- "Mountain Pass Rd" appears as one collection with a high score
- "Interstate 90" appears with a penalized score that is significantly lower than its raw score (0.3 multiplier)
- "Route 100" is split into two collections (straight gap > 2,414m)
- "Main Street" appears as two separate collections (geographically disconnected)
- Unnamed road does not appear in output

### 5. Test: Ordering and Filtering

- Collections are sorted by penalized score descending in the KML
- With `minScore` set to exclude low-scoring roads, they don't appear in the output
- "Mountain Pass Rd" outranks "Interstate 90" despite similar raw curvature (because of penalty)

### 6. Test: KML Loads Correctly

- Validate the output is well-formed XML using `xml.Unmarshal` into a generic structure.
- Verify no XML escaping issues with road names containing special characters (add a test road with `&` in its name, e.g., "Route 9 & 20").

### 7. Test: Edge Cases

- Empty input produces valid empty KML
- All roads below min-score produces valid empty KML
- Single-way road produces a single collection
- Road with all tier-0 segments produces a collection with score 0

## Acceptance Criteria

- [ ] Integration tests exercise the full stages 5-7 pipeline
- [ ] Test fixtures represent realistic road scenarios
- [ ] Aggregation correctly groups, splits, and scores
- [ ] Penalties correctly reduce motorway/trunk scores
- [ ] KML output is valid XML with correct structure
- [ ] Sort order, filtering, and edge cases verified
- [ ] All tests pass including with `-race` flag

## Notes

- These tests should be fast (no network calls, no file I/O except temp files). All data is synthetic.
- Use `bytes.Buffer` as the `io.Writer` for KML output to avoid temp files.
- Consider using `encoding/xml` to parse and verify the output rather than string matching — it's more robust.
- The test fixtures should use geographically plausible coordinates (e.g., Vermont area) so that distance calculations produce realistic results.
