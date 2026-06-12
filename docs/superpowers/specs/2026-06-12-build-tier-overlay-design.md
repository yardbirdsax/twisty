# Design: Tier-Colored Road Overlay for `twisty build`

**Date:** 2026-06-12
**Status:** Approved

## Overview

Embed a `diag serve`-style tier-colored road overlay into the `twisty build` interactive map. All cached roads in the viewport render with tier colors (green → yellow → orange → dark orange → red) at zoom 12+. Tiles are fetched and scored lazily as the user pans, using the same rate-limited pipeline as route scoring. Route legs start as a thin blue placeholder line and are visually replaced by the road overlay once scoring data arrives.

## New HTTP Endpoints

### `GET /api/segments?bbox=west,south,east,north`

Returns a GeoJSON FeatureCollection of all scored segments from tiles already in the cache that intersect the given bounding box.

- Reuses `collectionsToGeoJSON` and `filterFeaturesByBBox` from `diag_serve.go` directly (no duplication).
- Only reads from cache — does not trigger any tile fetches.
- Returns an empty FeatureCollection if no cached tiles intersect the bbox.

Response shape matches `diag serve`'s `/api/segments` exactly:
```json
{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "geometry": { "type": "LineString", "coordinates": [[lon, lat], [lon, lat]] },
      "properties": { "road_name": "PA 345", "tier": 2, "score": 14.3, "way_id": 123456 }
    }
  ]
}
```

### `GET /api/tiles?bbox=west,south,east,north`

Triggers background fetching of any uncached tiles that intersect the bbox. Uses the same `fetchMissingTiles` goroutine as route scoring — same rate limiting, same failure tracking in `failedTiles`.

- Returns `204 No Content` immediately (fetching is async).
- Already-cached tiles are ignored.
- No-op if all tiles in the bbox are already cached.

### `GET /api/segments/stream`

Server-Sent Events endpoint. Streams new scored segments to the browser as tiles finish loading.

- The `buildServer` struct gains a `tileReady chan quality.Tile` field and an SSE broadcaster.
- `fetchMissingTiles` sends on `tileReady` after each tile is successfully written to cache.
- The broadcaster reads from `tileReady`, scores the tile, converts to GeoJSON, and fans the event out to all connected SSE clients.
- Each SSE event is a single `data:` line containing a JSON-encoded GeoJSON FeatureCollection for that tile's segments.
- Clients that disconnect are cleaned up; the broadcaster continues running for the lifetime of the server.

## `buildServer` Changes

```go
type buildServer struct {
    // existing fields ...
    tileReady   chan quality.Tile
    sseBroker   *sseBroker  // fans out tileReady events to SSE clients
}
```

`sseBroker` is a simple fan-out: a `subscribe`/`unsubscribe` channel pair and a map of per-client `chan []byte` buffers. It runs in a single goroutine for the server lifetime.

`fetchMissingTiles` gains one new line after a tile is successfully cached:
```go
select {
case s.tileReady <- t:
default: // non-blocking; drop if no SSE clients connected
}
```

## Frontend Changes

### Road overlay layer

A new `L.geoJSON` layer (`roadLayer`) is added to the map immediately after the base tile layer (so route leg polylines and waypoint markers, added later, render on top of it). It uses the same `segmentStyle` function as `diag serve`:

```js
const TIER_COLORS = ['#475569', '#4ade80', '#facc15', '#fb923c', '#f87171'];
function segmentStyle(feature) {
  const tier = feature.properties.tier;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}
```

### Segment deduplication

A `Set<string>` (`renderedSegments`) tracks already-rendered segments using the key `"wayId:startLon:startLat"`. Before adding a feature to `roadLayer`, its key is checked — duplicates are skipped. This prevents redraws when the same tile is returned by both the bbox fetch and the SSE stream.

### On load and map movement

```
map.on('load moveend zoomend', function() {
  if (map.getZoom() < MIN_ZOOM) { roadLayer.clearLayers(); renderedSegments.clear(); return; }
  const bbox = getBboxString();
  fetch('/api/tiles?bbox=' + bbox);          // trigger background fetch (fire-and-forget)
  fetch('/api/segments?bbox=' + bbox)        // load already-cached segments
    .then(r => r.json())
    .then(fc => mergeFeatures(fc.features));
});
```

`MIN_ZOOM` is 12, matching `diag serve`.

### SSE connection

Opened once on page load:
```js
const evtSource = new EventSource('/api/segments/stream');
evtSource.onmessage = function(e) {
  mergeFeatures(JSON.parse(e.data).features);
};
```

### Route leg placeholder

When a leg is successfully routed (response from `/api/route-leg`), a thin blue polyline (`weight: 3, opacity: 0.5, color: '#2563eb'`) is drawn as a placeholder. It is stored in `legPolylines` as before. Once the road overlay renders segments covering those roads, the tier colors visually replace the blue line — the placeholder is not explicitly removed (it sits underneath the `roadLayer`).

The existing score polling loop is unchanged.

## Shared Code with `diag_serve.go`

`collectionsToGeoJSON`, `filterFeaturesByBBox`, `parseBBox`, `geoJSONFeature`, `geoJSONFeatureCollection`, `geoJSONSegmentProps`, and `geoJSONGeometry` are already defined in `diag_serve.go`. These will be moved to a shared file (e.g. `geojson.go`) so both `diag_serve.go` and `route_build.go` can use them without duplication.

## Zoom Behavior

- Below zoom 12: `roadLayer` is cleared and no fetches are issued.
- At zoom 12+: segments load on every `moveend`/`zoomend`. The bbox fetch only returns cached data; the `/api/tiles` call kicks off background fetching for any gaps.

## Rate Limiting

Map-pan tile fetches use the same `fetchMissingTiles` goroutine and the same `--fetch-delay` flag as route scoring. Concurrent calls to `fetchMissingTiles` (from panning and from waypoint routing) are safe — `failedTiles` uses `sync.Map` and the cache write is atomic at the file level.

## Error Handling

- `/api/segments`: returns an empty FeatureCollection on cache read errors (non-fatal).
- `/api/tiles`: fires and forgets; errors are logged to stderr as today.
- `/api/segments/stream`: if a tile fails to score, that tile is silently skipped in the broadcast (same behavior as today's score endpoint).
- SSE client disconnect: cleaned up by the broker's unsubscribe path.
