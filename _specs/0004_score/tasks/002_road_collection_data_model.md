# Task 002: Road Collection Data Model

## Summary

Define the `RoadCollection` struct and related types that represent the output of aggregation (stage 5) and carry data through penalties (stage 6) and KML rendering (stage 7). This establishes the data contract between pipeline stages.

## Dependencies

Task 001 — requires `ScoredWay` with `Tags` field.

## Detailed Directions

### 1. Create `quality/aggregate.go` with Data Model

- Create a new file `quality/aggregate.go`.
- Define the `RoadCollection` struct:

```go
// RoadCollection represents a named group of contiguous road segments
// that have been aggregated from individual scored ways.
type RoadCollection struct {
	Name         string
	SubIndex     int      // 0-based index when a named road is split into multiple collections
	HighwayTypes []string // unique highway types present in this collection
	WayIDs       []int64
	Segments     []ScoredSegment

	TotalScore  float64
	TotalLength float64 // meters
	ScorePerKm  float64

	// Populated by penalty stage (stage 6)
	PenaltyFactor    float64
	PenalizedScore   float64
	PenalizedPerKm   float64
}
```

- The `Segments` field reuses the existing `ScoredSegment` struct from `curvature.go`, which already has `Start`, `End`, `Tier`, `Score`, `Length`, etc.

### 2. Add Display Name Helper

- Add a method `DisplayName() string` on `RoadCollection` that returns the name with suffix when `SubIndex > 0`:
  - SubIndex 0: `"Route 100"`
  - SubIndex 1: `"Route 100 (2)"`
  - SubIndex 2: `"Route 100 (3)"`

### 3. Write Unit Tests

- Create `quality/aggregate_test.go` with tests for `DisplayName()`:
  - SubIndex 0 returns bare name
  - SubIndex > 0 returns name with 1-based suffix in parentheses

## Acceptance Criteria

- [ ] `RoadCollection` struct defined in `quality/aggregate.go`
- [ ] `DisplayName()` method works correctly for sub-indexed and non-sub-indexed collections
- [ ] Unit tests pass for `DisplayName()`
- [ ] Code compiles with no errors

## Notes

- The `PenaltyFactor`, `PenalizedScore`, and `PenalizedPerKm` fields will be zero-valued until stage 6 populates them. This is intentional — stages are additive.
- `HighwayTypes` is a deduplicated slice (not a map) for easy iteration and serialization.
