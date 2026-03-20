# Task 003: Hard Filter

## Summary

Implement the hard filter that removes roads unfit for twisty-road discovery: unpaved surfaces, private/restricted access, and non-motor-vehicle highway types. This is stage 2 of the pipeline, running before curvature scoring.

## Dependencies

None — uses only the existing `quality.Way` type.

## Detailed Directions

### 1. Create `quality/hardfilter.go`

- Create the file `quality/hardfilter.go` with package `quality`.
- Implement the function:

```go
// HardFilter returns only the ways that are suitable for curvature scoring.
// Ways with unpaved surfaces, private/restricted access, or non-motor-vehicle
// highway types are removed.
func HardFilter(ways []Way) []Way
```

- Internally, implement a helper:

```go
// isHardFiltered returns true if a way should be removed by the hard filter.
func isHardFiltered(tags map[string]string) bool
```

- Disqualification rules (OR'd — any match removes the way):
  1. **Unpaved surface**: `surface` tag is one of: `unpaved`, `gravel`, `dirt`, `mud`, `sand`.
  2. **Private/restricted access**: `access` tag is `private` or `no`, OR `motor_vehicle` tag is `no` or `private`.
  3. **Non-motor-vehicle highway**: `highway` tag is one of: `track`, `path`, `footway`, `cycleway`, `bridleway`, `steps`.

- A way with no `surface` tag is NOT disqualified by the surface rule.
- Note the overlap with the existing `IsDisqualifying()` in `overpass.go` — this is intentional. The hard filter is for the curvature pipeline and adds surface checks. Do NOT modify `IsDisqualifying()`; keep the two functions separate.

### 2. Write unit tests in `quality/hardfilter_test.go`

- Use table-driven tests.
- Test cases for `isHardFiltered`:
  - `surface=gravel` → filtered
  - `surface=dirt` → filtered
  - `surface=unpaved` → filtered
  - `surface=mud` → filtered
  - `surface=sand` → filtered
  - `surface=asphalt` → not filtered
  - `surface=paved` → not filtered
  - No `surface` tag → not filtered
  - `access=private` → filtered
  - `access=no` → filtered
  - `access=yes` → not filtered
  - `motor_vehicle=no` → filtered
  - `motor_vehicle=private` → filtered
  - `highway=track` → filtered
  - `highway=path` → filtered
  - `highway=footway` → filtered
  - `highway=cycleway` → filtered
  - `highway=bridleway` → filtered
  - `highway=steps` → filtered
  - `highway=residential` → not filtered
  - `highway=secondary` → not filtered
  - Multiple disqualifying tags → filtered (confirm OR logic)
  - No tags at all → not filtered
- Test `HardFilter` with a mixed slice of ways: verify that only non-disqualified ways are returned, and that the returned slice preserves order.
- Verify that the input slice is not modified (filter returns a new slice).

## Acceptance Criteria

- [ ] `quality/hardfilter.go` exists with `HardFilter` and `isHardFiltered`.
- [ ] `quality/hardfilter_test.go` passes with `go test ./quality/...`.
- [ ] All five unpaved surface values are caught.
- [ ] Access and motor_vehicle private/no are caught.
- [ ] All six non-motor-vehicle highway types are caught.
- [ ] Ways with no surface tag pass the filter.
- [ ] The existing `IsDisqualifying()` function is unchanged.

## Notes

- Keep the disqualifying values as local variables or small sets within the function — they don't need to be exported constants since they are not tunable scoring parameters.
- The filter is stateless: each way is evaluated independently.
