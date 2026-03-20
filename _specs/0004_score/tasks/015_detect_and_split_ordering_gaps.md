---
# Task 015: Detect and Split Way Ordering Gaps

## Summary

The greedy nearest-neighbor algorithm in `OrderWays` can produce chains where a way is appended that jumps far from the current chain end — typically because OSM has overlapping or duplicate ways for the same road. This causes stray lines in the KML output (e.g., Valley Creek Road jumps 2.4 km mid-polyline and retraces itself).

The fix is to add a post-ordering validation step that detects large gaps between consecutive ways in the chain and splits at those gaps, discarding the shorter fragment. This prevents stray lines from appearing in the rendered output.

## Dependencies

None — independent of other tasks.

## Detailed Directions

### 1. Add `SplitOrderingGaps` Function in `quality/aggregate.go`

After `OrderWays` produces an ordered chain, scan for gaps between consecutive ways where the distance from one way's last segment endpoint to the next way's first segment startpoint exceeds a threshold.

```go
// SplitOrderingGaps detects large gaps in an ordered way chain and returns
// only the longest contiguous sub-chain. A gap is defined as a distance
// between consecutive ways' shared endpoints exceeding maxGapM meters.
//
// This handles cases where OrderWays' greedy nearest-neighbor algorithm
// appends a way that jumps far from the chain end (e.g., overlapping OSM
// ways that retrace already-covered ground).
func SplitOrderingGaps(ordered []ScoredWay, maxGapM float64) []ScoredWay {
    if len(ordered) <= 1 {
        return ordered
    }

    // Find gap positions
    type chunk struct {
        start, end int // indices into ordered, inclusive
    }
    var chunks []chunk
    chunkStart := 0

    for i := 0; i < len(ordered)-1; i++ {
        _, curEnd, curOK := wayEndpoints(ordered[i])
        nextStart, _, nextOK := wayEndpoints(ordered[i+1])
        if !curOK || !nextOK {
            continue
        }
        dist := geo.Haversine(curEnd, nextStart)
        if dist > maxGapM {
            chunks = append(chunks, chunk{start: chunkStart, end: i})
            chunkStart = i + 1
        }
    }
    chunks = append(chunks, chunk{start: chunkStart, end: len(ordered) - 1})

    // Return the longest chunk
    best := chunks[0]
    bestLen := best.end - best.start + 1
    for _, c := range chunks[1:] {
        l := c.end - c.start + 1
        if l > bestLen {
            best = c
            bestLen = l
        }
    }

    return ordered[best.start : best.end+1]
}
```

### 2. Choose an Appropriate Gap Threshold

Use `ConnectedEndpointProximityM` (100m) as the gap threshold. This is the same distance used to determine whether two ways are "connected" in `FindConnectedComponents`. If two consecutive ways in the ordered chain have endpoints more than 100m apart, they shouldn't be chained together.

### 3. Integrate into the Pipeline

In `quality/aggregate.go`'s `Aggregate` function, call `SplitOrderingGaps` after `OrderWays` and before the deflection filter / `SplitAtStraightGaps`:

```go
for _, component := range components {
    ordered := OrderWays(component)
    ordered = SplitOrderingGaps(ordered, ConnectedEndpointProximityM)

    // deflection filter, split at straight gaps, etc.
    // ...
}
```

Similarly, in `pipeline.go`'s `processNameGroup` function, add the same call after `OrderWays`:

```go
ordered := quality.OrderWays(component)
ordered = quality.SplitOrderingGaps(ordered, quality.ConnectedEndpointProximityM)
```

### 4. Add Unit Tests

**`quality/aggregate_test.go`:**

- `TestSplitOrderingGaps_NoGaps`: Chain of 5 connected ways with endpoints < 100m apart. Returns all 5 ways unchanged.

- `TestSplitOrderingGaps_GapInMiddle`: Chain of 6 ways where ways 0-3 are connected, then a 2km gap, then ways 4-5 are connected. Returns ways 0-3 (the longer chunk).

- `TestSplitOrderingGaps_GapAtStart`: First way is far from the rest. Returns the longer chunk (ways 1-5).

- `TestSplitOrderingGaps_MultipleGaps`: Three chunks separated by gaps. Returns the longest chunk.

- `TestSplitOrderingGaps_SingleWay`: Returns the single way unchanged.

- `TestSplitOrderingGaps_EmptyInput`: Returns nil/empty.

### 5. Add a Regression Test for Valley Creek Road

Create a test fixture that mimics the Valley Creek Road scenario:
- 5 ways forming a north-to-south chain
- 1 way that overlaps with the middle of the chain (its nearest endpoint is close to the chain's end after greedy ordering, but creates a 2km jump)
- Verify that `SplitOrderingGaps` removes the overlapping way's retrace and the output chain has no large gaps

### 6. Consider Logging Dropped Ways

When ways are dropped by `SplitOrderingGaps`, log a debug message with the road name and dropped way IDs. This helps diagnose cases where legitimate ways are being dropped.

Add a variant or parameter to support logging:

```go
// In processNameGroup, after the split:
if len(ordered) != len(originalOrdered) {
    // Log dropped ways at debug level
}
```

This is optional but helpful for debugging.

## Acceptance Criteria

- [ ] `SplitOrderingGaps` detects gaps > `ConnectedEndpointProximityM` between consecutive ordered ways
- [ ] Returns the longest contiguous sub-chain, discarding shorter fragments
- [ ] Integrated into both `Aggregate` and `processNameGroup` after `OrderWays`
- [ ] Unit tests cover: no gaps, gap in middle, gap at start, multiple gaps, single way, empty input
- [ ] Regression test mimics the Valley Creek Road scenario (retrace after greedy ordering)
- [ ] `go test -race ./...` passes

## Notes

- The root cause is that `FindConnectedComponents` groups ways whose *endpoints* are within 100m, but `OrderWays` chains them greedily by nearest endpoint. If way A's end is near way B's start (so they're "connected"), but way B geographically overlaps with way C that's already in the chain, the greedy algorithm will happily append B after C, creating a jump back to B's start.
- An alternative fix would be to improve `OrderWays` itself to avoid creating gaps. However, post-ordering validation is simpler, more defensive, and catches any ordering pathology — not just this specific one.
- Dropping the shorter fragment is a conservative choice. The longer fragment is almost always the "real" road; the shorter one is typically an overlapping duplicate or a spur that shouldn't have been grouped.
- This does not require a score cache clear — it only affects post-aggregation processing.
