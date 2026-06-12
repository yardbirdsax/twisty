# Design: Way/Road Overlay Toggle for `twisty build`

**Date:** 2026-06-12
**Status:** Approved

## Overview

Add a toggle to the `twisty build` map that switches the road overlay between two coloring modes:

- **Ways mode** (default, existing behavior): each way segment colored by its individual tier (slate→green→yellow→orange→red, 0–4).
- **Roads mode** (new): each named road (`RoadCollection`) colored as a single uniform color using the yellow→red→magenta logarithmic gradient from `CurvatureColorLevel`/`GradientColor` — matching what `twisty score` produces in KML output.

The toggle is a button in the existing `#stats` control panel (top-right).

## Backend: `/api/road-segments`

### New endpoint

`GET /api/road-segments?bbox=west,south,east,north`

Returns a GeoJSON FeatureCollection where each feature represents one `RoadCollection`. The handler follows the same pattern as `handleSegments`:

1. Parse the `bbox` query param via `parseBBox`.
2. Find tiles in the bbox via `tilesInBBox`.
3. For each cached tile: `ParseTileData` → `RunScorePipeline` → `Aggregate`.
4. For each `RoadCollection`, compute a CSS color via `gradientColorCSS(CurvatureColorLevel(col.TotalScore, DefaultMinCurvature, DefaultMaxCurvature))`.
5. Emit one GeoJSON Feature per collection: a MultiLineString of all way coordinates, with properties `{ road_name, score, color }`.
6. Filter features by bbox via `filterFeaturesByBBox`, return as FeatureCollection.

No deduplication of `RoadCollection` instances across tiles is needed — roads that span tile boundaries will produce overlapping features, but since all ways in a collection share the same color, the overlap is invisible.

### `gradientColorCSS` helper

`GradientColor` currently returns KML `AABBGGRR` format. A new helper converts to CSS `#RRGGBB` for use in GeoJSON properties:

```go
func gradientColorCSS(level int) string {
    kml := GradientColor(level) // e.g. "FF00FFFF" = alpha,blue,green,red
    // KML AABBGGRR → CSS #RRGGBB: take bytes [6:8], [4:6], [2:4]
    return "#" + kml[6:8] + kml[4:6] + kml[2:4]
}
```

This helper lives in `geojson.go` alongside the other shared GeoJSON utilities, or in `quality/kml.go` next to `GradientColor` — whichever keeps the dependency clean (prefer `quality/kml.go` to avoid importing `quality` into `geojson.go` just for this).

### No SSE stream for road mode

Road mode is pull-only. A road can span multiple tiles; streaming partial roads would flash incomplete colors as tiles arrive. Road features are re-fetched on `moveend`/`zoomend` same as today's way mode bbox fetch.

## Frontend

### State

```js
var overlayMode = 'ways'; // 'ways' | 'roads'
```

### Toggle button

Added to `#stats` panel below the existing rows:

```html
<div class="row">
  <button id="btn-overlay-toggle" onclick="toggleOverlayMode()">Switch to Road view</button>
</div>
```

`toggleOverlayMode()`:
1. Flips `overlayMode` between `'ways'` and `'roads'`.
2. Updates button label: `'Switch to Road view'` when in ways mode, `'Switch to Way view'` when in roads mode.
3. Clears `roadLayer` and `renderedSegments`.
4. If switching to `'ways'`: reconnects SSE (`evtSource`), triggers bbox fetch to `/api/segments`.
5. If switching to `'roads'`: closes SSE connection, triggers bbox fetch to `/api/road-segments`.
6. The button is disabled below zoom 12 (same condition as the overlay).

### Style function

The existing `segmentStyle` reads `feature.properties.tier`. In road mode, features carry a precomputed `color` property instead. A single updated style function handles both:

```js
function segmentStyle(feature) {
  var color;
  if (overlayMode === 'roads') {
    color = feature.properties.color;
  } else {
    var tier = feature.properties.tier;
    color = TIER_COLORS[Math.max(0, Math.min(tier, 4))];
  }
  return { color: color, weight: 3, opacity: 0.8 };
}
```

### SSE management

The SSE `EventSource` is currently opened unconditionally on page load. With road mode, it must be:
- Opened on load (ways mode default).
- Closed (`evtSource.close()`) when switching to road mode.
- Re-opened when switching back to ways mode.

`evtSource` becomes a mutable variable (`var evtSource = null`) rather than a `const`, and a `connectSSE()` helper wraps the `new EventSource(...)` + `onmessage` setup.

### Road mode fetch

When in road mode, `loadSegments()` (the function called on `moveend`/`zoomend`) fetches `/api/road-segments?bbox=...` instead of `/api/segments?bbox=...`. The `mergeFeatures` call is identical — features are added to `roadLayer` using `segmentStyle`, which now reads `feature.properties.color`.

The deduplication key (`renderedSegments` Set) uses `"wayId:startLon:startLat"` for way mode. For road mode, features don't have `way_id`; since overlapping same-color features from adjacent tiles are invisible, deduplication uses `road_name` alone as the key — simple and sufficient.

## Zoom behavior

Below zoom 12: `roadLayer` is cleared, `renderedSegments` is cleared, and the toggle button is disabled. This is unchanged from current behavior; the new toggle just enables/disables alongside the existing zoom guard.

## Error handling

`/api/road-segments` returns an empty FeatureCollection on cache read or parse errors (same pattern as `/api/segments`). The frontend's `.catch()` on the fetch call is a no-op (silent failure), same as today.

## What does NOT change

- `/api/segments` and `/api/segments/stream` are untouched.
- The SSE broadcaster (`runTileSSEBroadcaster`) is untouched.
- Route leg polylines, waypoint markers, score polling, export, and clear route are untouched.
- The `--fetch-delay` rate limiting is untouched; `/api/road-segments` only reads from cache, never triggers fetches.
