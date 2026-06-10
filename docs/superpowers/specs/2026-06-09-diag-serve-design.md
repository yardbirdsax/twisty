# twisty diag serve — Visual Segment Debugger

**Date:** 2026-06-09

## Problem

When a road doesn't score as expected in `twisty score` output, there's no way to visually inspect individual segment scores. The existing `diag/` package provides Go test-based diagnostics, but they produce text output and require knowing the road name up front. There's no way to pan a map, hover over a road, and immediately see its tier and score.

## Solution

A `twisty diag serve` subcommand that reads the existing overpass cache, runs the full scoring pipeline, and serves an interactive map in the browser. Segments are drawn as colored polylines; hovering a segment shows a floating tooltip with the road name, tier, and score, and highlights all segments of that road.

## Command & Invocation

```
twisty diag serve [--cache-dir <path>] [--port <port>]
```

| Flag | Default | Description |
|---|---|---|
| `--cache-dir` | `~/.twisty/cache/overpass/` | Overpass tile cache directory |
| `--port` | `7777` | Port to listen on |

On startup the server prints `Listening on http://localhost:7777` and blocks until Ctrl-C. If the port is already in use the server exits immediately with a clear error.

## Data Pipeline

On startup:

1. Read all tiles from the cache directory using `diag.AllCachedTiles`
2. Exit hard with a clear error if the cache directory does not exist or contains zero tiles
3. Run `diag.SimulatePipelineFull` over all tiles to produce `[]RoadCollection`, each containing `[]ScoredSegment`
4. Hold the result in memory for the lifetime of the server

No live reloading — restart the server after running `twisty score` to see updated data.

## HTTP API

Two routes:

- `GET /` — serves the embedded HTML page
- `GET /api/segments` — returns a GeoJSON `FeatureCollection`

Each GeoJSON feature is a `LineString` (start → end coordinates of one `ScoredSegment`) with properties:

```json
{
  "road_name": "PA 120",
  "tier": 3,
  "score": 142.5,
  "way_id": 123456789
}
```

If individual tiles fail to parse they are skipped with a stderr warning; the rest of the data is still served.

## Frontend

A single HTML page embedded in the Go binary as a string constant. Stack:

- **Leaflet.js** via CDN — map rendering with OpenStreetMap tiles
- All segments drawn as Leaflet polylines on page load, colored by tier:
  - Tier 0: gray (`#475569`)
  - Tier 1: green (`#4ade80`)
  - Tier 2: yellow (`#facc15`)
  - Tier 3: orange (`#fb923c`)
  - Tier 4: red (`#f87171`)
- Default stroke weight: 3px, opacity: 0.8

**Hover behavior:**

- On `mouseover` a polyline: highlight all polylines sharing the same `road_name` (weight 5px, opacity 1.0); dim all others (weight 2px, opacity 0.2). Show a floating tooltip near the cursor with road name, tier, and score.
- On `mouseout`: restore all polylines to default weight/opacity; hide tooltip.

The tooltip is a small floating card (fixed position, follows cursor) showing:

```
PA 120
Tier 3 · Score 142.5
```

## Code Structure

- `main.go` — new `diag` subcommand dispatching to `diagServe()`
- `diag_serve.go` (package root) — `diagServe()` function handling flag parsing, cache loading, HTTP server setup
- HTML page as a `const` string in the same file
- Reuses `diag.AllCachedTiles`, `diag.SimulatePipelineFull`, `diag.DefaultCacheConfig`

## Testing

- **Unit test** in `diag/` (or package root): given synthetic `[]RoadCollection` input, verify the GeoJSON serialization produces features with correct geometry and properties
- **Integration test** using `httptest.NewServer`: verify `GET /` returns 200 with `Content-Type: text/html`, verify `GET /api/segments` returns 200 with valid GeoJSON

No automated tests for frontend hover behavior — verified manually.
