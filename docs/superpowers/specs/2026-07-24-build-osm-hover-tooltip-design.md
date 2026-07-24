# OSM Hover Tooltip — Design Spec

**Date:** 2026-07-24

## Overview

When the map is in **Way view** mode, hovering over a road segment shows a small tooltip with OSM tag data for that way: `highway` type and `maxspeed`. The feature is always active in Way view — no toggle needed.

## Scope

- Way view only (`OVERLAY_WAYS`). Tooltip does nothing in Road view.
- Tags shown: `highway`, `maxspeed`.
- Data source: GeoJSON feature properties (enriched server-side, no extra API calls on hover).

## Backend Changes

### `geojson.go` — `geoJSONSegmentProps`

Add two new pointer fields:

```go
type geoJSONSegmentProps struct {
    RoadName string   `json:"road_name"`
    Tier     int      `json:"tier"`
    Score    float64  `json:"score"`
    WayID    int64    `json:"way_id"`
    Highway  *string  `json:"highway,omitempty"`
    Maxspeed *string  `json:"maxspeed,omitempty"`
}
```

Pointers with `omitempty` so features with no `maxspeed` tag don't bloat the JSON.

### `geojson.go` — `collectionsToGeoJSON`

Add a `ways quality.Ways` parameter. For each segment, look up `ways[seg.WayID]` and populate `Highway` and `Maxspeed` from `way.Tags["highway"]` and `way.Tags["maxspeed"]`.

```go
func collectionsToGeoJSON(collections []quality.RoadCollection, ways quality.Ways) geoJSONFeatureCollection {
```

Both callers pass the `ways` map they already have in scope:
- `handleSegments` (in `route_build.go`)
- `runTileSSEBroadcaster` (in `route_build.go`)

## Frontend Changes (in `buildHTML` in `route_build.go`)

### Tooltip div

Added to `<body>` (after `#toast`):

```html
<div id="osm-tooltip" style="
  display:none; position:fixed; z-index:2000;
  background:rgba(255,255,255,0.95); border-radius:8px;
  padding:8px 12px; box-shadow:0 2px 8px rgba(0,0,0,0.15);
  font-size:12px; pointer-events:none; min-width:120px;
"></div>
```

`pointer-events:none` prevents the tooltip from triggering its own mouseout events. `position:fixed` so it follows the cursor regardless of map scroll.

### JS — tooltip wiring

`roadLayer` is created once on page load. After creation, attach:

```js
roadLayer.on('mouseover', function(e) {
  if (overlayMode !== OVERLAY_WAYS) return;
  var p = e.layer.feature && e.layer.feature.properties;
  var hw = (p && p.highway) || '—';
  var ms = (p && p.maxspeed) || '—';
  var el = document.getElementById('osm-tooltip');
  el.innerHTML = '<b>' + hw + '</b><br>Max speed: ' + ms;
  el.style.display = 'block';
});

roadLayer.on('mouseout', function() {
  document.getElementById('osm-tooltip').style.display = 'none';
});
```

A `mousemove` handler on the map keeps the tooltip near the cursor:

```js
map.on('mousemove', function(e) {
  var el = document.getElementById('osm-tooltip');
  if (el.style.display === 'none') return;
  var offset = 14;
  el.style.left = (e.originalEvent.clientX + offset) + 'px';
  el.style.top  = (e.originalEvent.clientY + offset) + 'px';
});
```

### JS — hide on mode toggle

In `toggleOverlayMode()`, add:

```js
document.getElementById('osm-tooltip').style.display = 'none';
```

### JS — hide on overlay hidden

In `toggleOverlayVisibility()` when hiding the overlay, add:

```js
document.getElementById('osm-tooltip').style.display = 'none';
```

## Edge Cases

| Case | Behavior |
|------|----------|
| `highway` tag absent | Show "—" (shouldn't happen given `HighwayFilter`, but defensive) |
| `maxspeed` tag absent | Show "—" (common) |
| Hovering in Road view | Tooltip does not appear (`overlayMode` check) |
| Overlay hidden | Tooltip hidden immediately on hide |
| Mode toggled away from Ways | Tooltip hidden immediately |

## No New Tests Needed

The only new server-side logic is reading two strings from an existing map — covered implicitly by the existing `collectionsToGeoJSON` tests once the signature changes. The tooltip itself is pure UI behavior tested manually.
