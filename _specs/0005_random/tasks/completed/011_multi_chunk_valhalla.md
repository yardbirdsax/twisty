---
# Task 011: Multi-Chunk Valhalla Routing

## Summary

Valhalla's `/route` endpoint has a hard 20-location limit. The loop route uses `[start, wp1, ..., wpN, start]`, leaving room for only 18 intermediate waypoints. The `ChainSelector` builds chains with potentially 50+ dense waypoints, but `subsampleWaypoints` crushes them to 18, losing the boundary points and inter-collection guidance that prevent backtracking. The result: Valhalla fills the massive gaps between sparse waypoints with backtracking, producing routes that cover ~30km of unique ground in ~2h.

The fix: split the waypoint list across multiple Valhalla `/route` calls, each with ≤20 locations, then stitch the results. This removes the waypoint cap from the chain selector entirely.

## Dependencies

- Task 010 (boundary waypoint fix — the waypoints this task sends to Valhalla)
- Task 003 (`FetchLoopRouteFromURL` and `stitchLegs` — the code being modified)

## Detailed Directions

### 1. Remove `maxWaypointCount` and `subsampleWaypoints` from `waypoint/chain_selector.go`

- Delete the `maxWaypointCount` constant (line 15).
- Delete the `subsampleWaypoints` function (around line 450).
- In the `Select` method, change the return from:
  ```go
  waypoints := extractDenseWaypoints(chain, waypointInterval)
  return subsampleWaypoints(waypoints, maxWaypointCount)
  ```
  to:
  ```go
  return extractDenseWaypoints(chain, waypointInterval)
  ```

### 2. Remove subsample tests from `waypoint/chain_selector_test.go`

Delete:
- `TestChainSelector_Subsample`
- `TestChainSelector_SubsampleNoOp`
- `TestChainSelector_WaypointCap`

These tests validate `subsampleWaypoints` which is being removed.

### 3. Add chunked routing to `route/valhalla_loop.go`

Add a constant:
```go
const maxValhallaLocations = 20
```

Refactor `fetchLoopRouteFromURL` to:

1. Build the full location list: `[start, wp1, ..., wpN, start]`.
2. If total locations ≤ `maxValhallaLocations`, make a single call as today.
3. If > `maxValhallaLocations`, split into overlapping chunks where the last location of chunk N is the first location of chunk N+1:
   - Chunk 1: `locations[0..19]` (20 locations)
   - Chunk 2: `locations[19..38]` (last of chunk 1 = first of chunk 2)
   - ...
   - Last chunk: `locations[k..end]` (may have fewer than 20)
4. For each chunk, issue a Valhalla `/route` call using the same costing options.
5. Stitch all chunk results:
   - Concatenate decoded points (dropping the duplicate boundary point between chunks).
   - Sum durations and distances across all chunks.
6. Score the combined route.

Extract the single-call logic into a helper so both paths share parsing/error handling:

```go
// routeChunk issues a single Valhalla /route call for a set of locations
// and returns the stitched points, total time, and total distance.
func routeChunk(baseURL string, locations []valhallaLocation) ([]geo.Coord, float64, float64, error)
```

### 4. Add tests to `route/valhalla_loop_test.go`

- **TestChunkedRoute_ManyWaypoints**: 40 waypoints → verify multiple Valhalla calls are made (use a test HTTP server that counts calls), results stitched correctly.
- **TestChunkedRoute_ExactlyAtLimit**: 18 waypoints → 20 locations → single call, no chunking.
- **TestChunkedRoute_OneOverLimit**: 19 waypoints → 21 locations → 2 calls (20 + 2).
- **TestChunkedRoute_BoundaryStitching**: Verify no duplicate points at chunk boundaries.

### 5. Verify existing tests

Existing `valhalla_loop_test.go` tests use small location counts and should pass unchanged since the ≤20 path is identical to the current implementation.

## Acceptance Criteria

- [ ] `maxWaypointCount` constant and `subsampleWaypoints` function removed from `chain_selector.go`.
- [ ] `Select` returns `extractDenseWaypoints` output directly.
- [ ] `fetchLoopRouteFromURL` splits locations into ≤20-location chunks when needed.
- [ ] Chunks overlap by 1 location (last of chunk N = first of chunk N+1).
- [ ] Points are stitched with no duplicates at chunk boundaries.
- [ ] Duration and distance are summed across all chunks.
- [ ] Routes with ≤20 locations use a single call (no regression).
- [ ] New tests verify chunking at various sizes.
- [ ] `go test ./waypoint/... ./route/...` passes.
- [ ] `go vet ./...` clean.

## Notes

- The number of Valhalla calls scales linearly with waypoint count. A 2h ride with ~60 waypoints would require ~4 calls. This is acceptable — each call takes ~1s and the total is still fast.
- The public Valhalla server (valhalla1.openstreetmap.de) may rate-limit concurrent requests. Chunks should be issued sequentially, not in parallel.
- The `stitchLegs` function already handles multi-leg responses correctly (task 003). Each chunk will produce its own multi-leg response; the chunk-level stitching is a second layer on top.
