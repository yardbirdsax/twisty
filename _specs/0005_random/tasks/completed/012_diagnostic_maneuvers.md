# Task 012: Diagnostic — GPX with turn-by-turn maneuvers + per-chunk logging

## Status: completed

## Summary

Added Valhalla maneuver extraction and GPX waypoint output to support diagnosing
suspicious route durations. Per-chunk logging was added to the chunked routing path.

## Changes

### `route/valhalla.go`
- Added `valhallaManeuver` struct with `Instruction`, `StreetNames`, `Time`,
  `Length`, `BeginShapeIndex`, `Type` fields.
- Added `Maneuvers []valhallaManeuver` field to `valhallaLeg`.

### `route/osrm.go`
- Added public `Maneuver` struct with `Instruction`, `StreetNames`, `TimeSec`,
  `LengthKm`, `Location` fields.
- Added `Maneuvers []Maneuver` field to the `Route` struct.

### `route/valhalla_loop.go`
- Updated `routeChunk` signature to return `([]geo.Coord, float64, float64, []Maneuver, error)`.
- Inlined per-leg point decoding in `routeChunk` so `BeginShapeIndex` can be
  resolved to a `geo.Coord` for each maneuver.
- Updated `fetchLoopRouteFromURL` to collect and propagate maneuvers across chunks.
- Added per-chunk stderr logging (fires only in the chunked path, i.e. >20 locations):
  `chunk N: M locations, X.Xkm, Ys`
- Added `"os"` to imports.

### `gpx/gpx.go`
- Added `Waypoint` struct with `Lat`, `Lon`, `Name`, `Desc` fields.
- Added `Wpts []Waypoint` field to `GPX` struct (before `Trk` per GPX schema).
- Added `WriteGPXWithWaypoints` function — same as `WriteGPX` but populates waypoints.
- `WriteGPX` is unchanged.

### `main.go`
- For the `random` command GPX output, replaced `gpx.WriteGPX` with
  `gpx.WriteGPXWithWaypoints`.
- Converts `Route.Maneuvers` to `[]gpx.Waypoint`: name = instruction (truncated
  to 40 chars), desc = full instruction + duration + distance.

### `gpx/gpx_test.go`
- Added `TestWriteGPXWithWaypoints` — verifies `<wpt>` elements with lat, lon,
  name, desc appear in output and round-trip via XML unmarshal.

### `route/valhalla_loop_test.go`
- Added `TestFetchLoopRoute_ManeuversPopulated` — verifies maneuver count,
  instruction text, timing, and resolved `Location` from `BeginShapeIndex`.

## Verification

```
go vet ./...           # clean
go build ./...         # clean
go test ./gpx/... ./route/... ./waypoint/... -run "^Test[^I]|^TestI[^n]"
# ok gpx, ok route, ok waypoint
```

Integration test `TestFetchRoutesValhalla_Integration` hit a 429 rate limit from
the live endpoint during development — unrelated to this task.
