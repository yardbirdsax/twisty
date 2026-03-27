---
# Task 010: Fix `extractDenseWaypoints` to emit per-collection boundary points

## Summary

Fix a bug in `extractDenseWaypoints` where only the first point of the first collection and the last point of the last collection are emitted as waypoints. The task 008 spec requires the first and last points of **every** collection to be emitted, so Valhalla receives explicit guidance at each collection transition. Additionally, the accumulated distance counter carries over across collection boundaries without resetting, causing the first waypoint inside a new collection to appear at an unpredictable offset rather than at the collection's entry point.

The result of this bug is short in-and-out spur segments in the GPX output. Without boundary waypoints, Valhalla must fill the gap between the last interval-emitted waypoint in collection A and the first interval-emitted waypoint in collection B. In many cases, the shortest path overshoots the collection boundary and backtracks — creating spurs like the Seisholtzville Rd → Constitution Dr out-and-back, the Mulberry Hill Rd spur, and the Salem Bible Church Rd spur observed in field testing.

## Dependencies

- Task 008 (ChainSelector implementation — the code being fixed)

## Detailed Directions

### 1. Modify `extractDenseWaypoints` in `waypoint/chain_selector.go`

Replace the current implementation (lines 319–356) with a per-collection loop that:

1. For each collection with segments:
   a. Emits the collection's first segment start point (deduplicated against the last emitted waypoint).
   b. Resets `accumulated = 0.0`.
   c. Walks segments, emitting a waypoint every `intervalM` meters (same as today).
   d. Emits the collection's last segment end point (deduplicated).

2. Removes the separate "first point of first collection" and "last point of last collection" logic — both are subsumed by the per-collection entry/exit emissions.

```go
func extractDenseWaypoints(chain []quality.RoadCollection, intervalM float64) []geo.Coord {
    if len(chain) == 0 {
        return nil
    }

    var waypoints []geo.Coord

    for _, c := range chain {
        if len(c.Segments) == 0 {
            continue
        }

        // Emit entry point of this collection.
        entry := c.Segments[0].Start
        if len(waypoints) == 0 || geo.Haversine(waypoints[len(waypoints)-1], entry) > deduplicateProximityM {
            waypoints = append(waypoints, entry)
        }

        // Walk segments at interval spacing, reset per collection.
        accumulated := 0.0
        for _, seg := range c.Segments {
            accumulated += seg.Length
            if accumulated >= intervalM {
                wp := seg.End
                if len(waypoints) == 0 || geo.Haversine(waypoints[len(waypoints)-1], wp) > deduplicateProximityM {
                    waypoints = append(waypoints, wp)
                }
                accumulated = 0.0
            }
        }

        // Emit exit point of this collection.
        exit := c.Segments[len(c.Segments)-1].End
        if len(waypoints) == 0 || geo.Haversine(waypoints[len(waypoints)-1], exit) > deduplicateProximityM {
            waypoints = append(waypoints, exit)
        }
    }

    return waypoints
}
```

### 2. Update existing tests in `waypoint/chain_selector_test.go`

- **`TestChainSelector_DenseWaypoints`:** Single-collection behavior is nearly identical (first and last were already emitted). Verify the expected count still holds — it should, since the accumulated counter reset only matters across collection boundaries.
- **`TestChainSelector_ExtractDenseWaypoints_MultipleCollections`:** Add explicit checks that the first point of collection 2 and the last point of collection 1 appear as waypoints in the output.

### 3. Add new test: `TestExtractDenseWaypoints_BoundaryPointsEmitted`

Create 3 collections at distinct, well-separated positions. Use a large `intervalM` (e.g., 10000m) so that no interval-based waypoints are emitted — only boundary points. Assert that the output contains exactly 6 waypoints: entry and exit of each collection, in order.

## Acceptance Criteria

- [ ] `extractDenseWaypoints` emits the first and last segment points of every collection in the chain (not just the first/last of the entire chain).
- [ ] The accumulated distance counter resets to 0 at the start of each collection.
- [ ] Deduplication still applies — boundary points within 100m of the previous waypoint are skipped.
- [ ] Single-collection behavior is unchanged (test verified).
- [ ] Multi-collection chains have boundary waypoints at every collection transition (test verified).
- [ ] New test with large interval confirms only boundary points are emitted (test verified).
- [ ] `go test ./waypoint/...` passes.
- [ ] `go vet ./waypoint/...` clean.

## Notes

- This is a bugfix, not a behavioral change. The task 008 spec already requires per-collection boundary emission; the implementation simply missed it.
- The `subsampleWaypoints` cap (20 waypoints) still applies downstream. With more waypoints emitted per chain, the cap may be hit more often, which is acceptable — the subsampler preserves first/last and spaces evenly.
- The deduplication threshold (100m) may cause some boundary points to be skipped if consecutive collections share endpoints or are very close. This is correct behavior — it avoids feeding Valhalla near-duplicate waypoints.
