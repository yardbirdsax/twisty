# Task 013: Chain selector spatial coherence fixes

## Status: completed

## Summary

Four targeted fixes to `waypoint/chain_selector.go` eliminate the zigzag waypoint
sequences the chain selector was producing. Visual inspection via Google My Maps
overlay revealed chains going 5km out, returning to origin, going 9km in another
direction, oscillating — caused by four compounding bugs.

## Changes

### `waypoint/chain_selector.go`

- **(a) Outbound distance floor.** In the chain loop, before the connector distance
  calculation, skip candidates whose `distanceFromStart` is less than 80% of
  `currentPos`'s distance from origin when `cumulativeTimeSec <= timeBudgetSec*0.4`.
  Enforces monotonic outward progress during the outbound leg.

- **(b) Trajectory-relative bearing.** Changed `geo.Bearing(origin, e.midpoint)` to
  `geo.Bearing(currentPos, e.midpoint)` for the bearing sweep factor. The sweep now
  penalizes candidates that require going backward from the current position rather
  than from the origin, which was ineffective for near-origin collections.

- **(c) Connector to nearest endpoint.** Replaced `geo.Haversine(currentPos, e.midpoint)`
  with the minimum of `geo.Haversine(currentPos, start)` and `geo.Haversine(currentPos, end)`
  for both the scoring connector distance and the post-selection accounting connector
  distance. The return time estimate now uses the far endpoint after hypothetical
  orientation rather than the raw midpoint. Applied at both the scoring loop and the
  post-selection `connectorTimeSec` calculation.

- **(d) Global waypoint deduplication.** Added `coordKey` struct and `toCoordKey` helper
  (10000x lat/lon → ~11m grid resolution). Replaced consecutive-only deduplication in
  `extractDenseWaypoints` with an `appendWP` closure that checks both the consecutive
  proximity threshold and a `seen map[coordKey]bool` covering all previously emitted
  waypoints. Prevents Valhalla U-turns when the chain revisits a geographic area.

- **Remove DEBUG break.** Removed the `// DEBUG: bypass retry loop` unconditional
  `break` from the retry loop in `main.go`, re-enabling multi-attempt route search.

### `waypoint/chain_selector_test.go`

- Added `TestExtractDenseWaypoints_GlobalDedup` — constructs a chain A→C→B where A and
  B share the same entry point (geographic duplicate separated by a diversion C). Verifies
  the shared point appears exactly once in the extracted waypoints, confirming global dedup
  suppresses non-consecutive duplicates that the old consecutive-only check would miss.

### `_specs/0005_random/prd.md`

- Added "2026-03-23: Spatial coherence fixes for chain ordering" amendment after the
  collection orientation amendment, documenting all four root causes and the decision.

## Verification

```
go vet ./waypoint/...   # clean
go build ./...          # clean
go test ./waypoint/...  # ok
```
