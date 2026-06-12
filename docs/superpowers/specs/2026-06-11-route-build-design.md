# Route Build — Interactive Route Builder Web UI

## Overview

A new subcommand `twisty route build` that serves a local web UI for interactively constructing driving routes by clicking waypoints on a map. Routes are rendered in real-time using Valhalla, and scored for curvature using the existing tile-based scoring pipeline. The user can export the final route as GPX or KML.

## Command & Flags

```
twisty route build --address <addr> [--port 8080] [--overpass-url <url>] [--cache-dir <path>] [--tile-size 0.1] [--fetch-delay 1s]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--address` (required) | — | Center address for initial map view |
| `--port` | `8080` | Local HTTP server port |
| `--overpass-url` | `https://overpass-api.de/api/interpreter` | Overpass API endpoint for tile fetching |
| `--cache-dir` | `~/.twisty/cache/overpass/` | Tile cache directory |
| `--tile-size` | `0.1` | Tile size in degrees for grid-based fetching |
| `--fetch-delay` | `1s` | Delay between Overpass tile fetches |

On startup: geocodes the address, prints the server URL to stdout, and begins serving.

## Architecture

Single-file embedded HTML pattern (same as `diag serve`). One Go file (`route_build.go`) contains the HTTP server, API handlers, and the full frontend as an embedded HTML string with inline JS/CSS.

### Backend API

| Method | Endpoint | Request | Response |
|--------|----------|---------|----------|
| `GET` | `/` | — | HTML page |
| `POST` | `/api/route-leg` | `{from: {lat, lon}, to: {lat, lon}}` | `{points: [[lat,lon],...], duration: float64, distance: float64}` |
| `POST` | `/api/score` | `{points: [[lat,lon],...]}` | `{score: float64, pending_tiles: int}` |
| `POST` | `/api/export` | `{format, waypoints, legs}` | File download |

#### POST /api/route-leg

Calls `route.FetchRoutes(from, to)` via Valhalla. Returns the geometry of the first route plus its duration (seconds) and distance (meters). Only routes the single new leg — does not re-route the full chain.

#### POST /api/score

Takes the full concatenated route polyline. Identifies which tiles it crosses (using the same grid logic as `twisty score`), checks the score cache:

- **Cache hit**: Runs the aggregation pipeline on cached tile data. Applies a proximity filter — only scores OSM segments whose midpoint is within ~50m of the route polyline. Returns the sum.
- **Cache miss**: Kicks off background goroutines to fetch missing tiles from the Overpass URL, score them, and write to cache. Returns `pending_tiles > 0` so the frontend polls.

On re-poll, more tiles will be cached, pending count drops. When 0, the score is final.

#### POST /api/export

Takes the full route data (waypoints + leg geometries) as a JSON body. Writes GPX or KML from the already-computed geometry — no additional Valhalla calls needed. Returns the file with `Content-Disposition: attachment` header.

Request: `{format: "gpx"|"kml", waypoints: [{lat, lon},...], legs: [{points: [[lat,lon],...]},...]}` 

## Frontend

### Technology

- Leaflet 1.9.4 with OpenStreetMap tiles
- Vanilla JavaScript, no build step
- Embedded in Go source as a const string

### UI Layout

- Full-screen Leaflet map
- **Top-right overlay**: Running totals (twist score, distance, time) + tile fetch status indicator
- **Bottom-right**: Export GPX / Export KML buttons (disabled until >= 2 waypoints)
- **Bottom-left**: Instruction hint ("Click map to add waypoint · Click last marker to undo")

### Waypoint Markers

- **Green** (#16a34a): Start point (waypoint 1)
- **Blue** (#2563eb): Intermediate waypoints
- **Red with glow** (#dc2626): Last waypoint (visually signals it's removable)

### State

```javascript
waypoints[]    // {lat, lon} clicked by user
legs[]         // {points, duration, distance} returned per leg
totalScore     // running sum from /api/score
totalDistance   // sum of leg distances
totalTime      // sum of leg durations
pendingTiles   // tiles still being fetched/scored
```

### Click Flow (add waypoint)

1. Click on map -> append to `waypoints[]`
2. If `waypoints.length >= 2`, POST `/api/route-leg` with last two points
3. On response: push to `legs[]`, draw polyline on map, update distance/time
4. POST full concatenated points to `/api/score`
5. Update score; if `pending_tiles > 0`, poll `/api/score` every 2s until 0

### Undo Flow (remove last waypoint)

1. Click last marker -> pop from `waypoints[]`, remove last leg polyline from map
2. Recalculate distance/time from remaining `legs[]`
3. Re-POST remaining route to `/api/score` for updated twist score

### Export Flow

1. Click Export button -> POST full route data (waypoints + legs) to `/api/export`
2. Server returns file as a download (browser triggers save dialog)

## Scoring Integration

The scoring uses the full tile-based pipeline (`quality/` package) for high-fidelity results consistent with `twisty score` output:

1. **Tile identification**: Compute which tiles the route polyline passes through at `--tile-size` granularity
2. **Cache check**: Look up each tile in `--cache-dir`
3. **Proximity filter**: Only score OSM segments whose midpoint is within ~50m of the route polyline
4. **Background fetch**: Missing tiles are fetched from the Overpass URL asynchronously; the frontend shows "Scoring N tiles..." and polls until complete
5. **Aggregation**: Scored segments are run through the standard pipeline (deflection filter, penalties) and summed

Pre-warming the cache with `twisty fetch` gives instant scoring. Cache misses introduce latency only for the affected tiles.

## Error Handling

- **Valhalla unreachable/error**: Show toast on map ("Could not route between these points"), don't add the waypoint
- **Overpass fetch fails for a tile**: Mark tile as failed, show "N tiles failed" in overlay. Route geometry and time/distance still display; twist score will be incomplete.
- **Geocode fails on startup**: Print error to stderr and exit

No retry logic. If a leg fails, the user clicks somewhere else.

## File Structure

### New files

- `route_build.go` — Command setup, HTTP server, API handlers, embedded HTML/JS/CSS

### Modified files

- `main.go` — Register the `route build` subcommand

### Reused existing code

- `route/valhalla.go` — `FetchRoutes()` for per-leg routing
- `quality/` package — Full scoring pipeline (hard filter, curvature, deflection, aggregation, penalties)
- `overpass.go` — Tile fetching and caching
- `geo/` package — Coordinate utilities, Haversine, polyline decoding
- `geocode/` package — Address geocoding

## Future Enhancements (out of scope)

- Click any waypoint to truncate from that point (currently: remove last only)
- Color-code route segments by curvature tier (like `--multi-color` in `twisty score`)
- Drag waypoints to reposition
- "Analyze with full pipeline" button for detailed per-road breakdown
