# Task 004: Per-Segment Curvature Scoring

## Summary

Implement the per-segment curvature scoring algorithm that assigns a curvature score to every segment of every way. This is the core of stage 3: iterate through node triples, compute circumradii, assign tiers, and produce weighted segment scores.

## Dependencies

- Task 001 (scoring parameters — `AssignTier`)
- Task 002 (circumradius computation — `geo.Circumradius`)

## Detailed Directions

### 1. Define the scored segment data model

- Create the file `quality/curvature.go` with package `quality`.
- Define the types:

```go
// ScoredSegment holds the curvature score for a single segment between two nodes.
type ScoredSegment struct {
    Start  geo.Coord
    End    geo.Coord
    Radius float64 // circumradius in meters; +Inf for straight segments
    Tier   int
    Weight float64
    Length float64 // Haversine distance in meters
    Score  float64 // Length * Weight
}

// ScoredWay holds scored segments for a single OSM way.
type ScoredWay struct {
    WayID    int64
    Segments []ScoredSegment
}
```

### 2. Implement the scoring function

- Implement:

```go
// ScoreWay computes curvature scores for all segments in a way.
// Ways with fewer than 3 nodes receive zero-score segments.
func ScoreWay(w Way) ScoredWay
```

- Algorithm:
  1. If `len(w.Geometry) < 2`, return a `ScoredWay` with no segments.
  2. If `len(w.Geometry) == 2`, return one segment with `Radius=+Inf`, tier 0, weight 0, score 0.
  3. For ways with 3+ nodes, compute circumradii for all consecutive triples `(i, i+1, i+2)`.
  4. For each interior segment `(i, i+1)` that participates in two triangles `(i-1, i, i+1)` and `(i, i+1, i+2)`, take the **minimum** circumradius (tightest curve).
  5. The first segment `(0, 1)` only participates in triangle `(0, 1, 2)` — use that single radius.
  6. The last segment `(n-2, n-1)` only participates in triangle `(n-3, n-2, n-1)` — use that single radius.
  7. For each segment, call `AssignTier(radius)` to get tier and weight.
  8. Compute `Length = geo.Haversine(start, end)` and `Score = Length * Weight`.

- Implement a batch function:

```go
// ScoreWays scores all ways and returns scored results.
func ScoreWays(ways []Way) []ScoredWay
```

### 3. Write unit tests in `quality/curvature_test.go`

- **Two-node way**: should produce one segment with zero score.
- **Three-node way (straight line)**: all three points collinear → circumradius is +Inf → tier 0 → score 0.
- **Three-node way (tight curve)**: three points forming a tight curve → verify tier and score are non-zero and correct.
- **Four-node way (overlapping triangles)**: verify that the interior segment gets the minimum radius from its two triangles.
- **Five+ node way with mixed curvature**: a way with some straight and some curvy segments. Verify that straight segments score 0 and curvy segments score > 0.
- **One-node way**: no segments produced.
- **Empty geometry**: no segments produced.
- **Score calculation**: verify that `Score = Length * Weight` for a known segment.
- For non-degenerate cases, use tolerance-based comparison (within 2%) since Haversine introduces small approximation errors.

## Acceptance Criteria

- [ ] `quality/curvature.go` exists with `ScoredSegment`, `ScoredWay`, `ScoreWay`, and `ScoreWays`.
- [ ] `quality/curvature_test.go` passes with `go test ./quality/...`.
- [ ] Ways with < 3 nodes produce zero-score segments.
- [ ] Interior segments use the minimum circumradius from overlapping triangles.
- [ ] Segment scores equal `length * weight`.
- [ ] Tier assignment uses the constants from `scoring_params.go`.

## Notes

- The "minimum circumradius" rule biases toward detecting curves. This is intentional per the PRD's "bias toward detecting curves" design principle.
- Keep the implementation straightforward: a single pass to compute all circumradii, then a pass to assign min-radius to each segment. No need for complex data structures.
- The `ScoredSegment` struct preserves `Start`/`End` coordinates because downstream stages (KML output, aggregation) need segment geometry.
