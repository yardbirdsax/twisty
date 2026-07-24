# Min Speed Slider for Road Overlay

**Date:** 2026-07-23
**Status:** Approved

## Overview

Add a "Min speed" slider to the build UI stats panel that filters the road overlay to show only roads whose representative speed limit meets a minimum threshold. The slider ranges from 0 (no filter) to 100 mph. It works in AND with the existing min-twist slider: a road must meet both thresholds to appear.

## Scope

- New `max_speed_mph` field on road GeoJSON properties (backend)
- New `WeightedAverageSpeedMPH` helper in `quality/maxspeed.go`
- New slider HTML + JS in `route_build.go`
- No new API endpoints; no query param changes

## Backend

### Helper: `WeightedAverageSpeedMPH`

Add to `quality/maxspeed.go`:

```go
// WeightedAverageSpeedMPH returns the length-weighted average speed (in mph)
// across all WaySpeedInfo entries that have a tagged speed. Returns (0, false)
// if no entries have a speed tag.
func WeightedAverageSpeedMPH(speeds []WaySpeedInfo) (mph float64, ok bool)
```

Iterates `speeds`, accumulating `SpeedMPH * LengthM` and `LengthM` for entries where `HasSpeed == true`. Returns `sum / totalLength, true` if `totalLength > 0`, else `0, false`.

### GeoJSON properties

`geoJSONRoadProps` in `geojson.go` gains:

```go
MaxSpeedMPH *float64 `json:"max_speed_mph"`
```

A pointer so roads with no tagged speed serialize as `null` rather than `0` (which would be ambiguous).

`collectionsToRoadGeoJSON` calls `quality.WeightedAverageSpeedMPH(c.WaySpeeds)` and sets the field:

```go
var maxSpeedMPH *float64
if mph, ok := quality.WeightedAverageSpeedMPH(c.WaySpeeds); ok {
    maxSpeedMPH = &mph
}
// ... in the feature append:
Properties: geoJSONRoadProps{
    RoadName:    c.DisplayName(),
    Score:       c.TotalScore,
    Color:       color,
    MaxSpeedMPH: maxSpeedMPH,
},
```

## Frontend

### HTML

A new slider row is inserted directly below the existing `#min-twist-row` in the stats panel:

```html
<div id="min-speed-row" class="row" style="margin-top:4px;display:block;">
  <label for="min-speed-slider"><span>Min speed</span><span id="min-speed-label">0 mph</span></label>
  <input id="min-speed-slider" type="range" min="0" max="100" step="1" value="0" disabled oninput="onMinSpeedInput(this.value)">
</div>
```

The slider starts disabled (enabled only in OVERLAY_ROADS mode, same as min-twist).

### JavaScript

New global variable:

```js
var minSpeedFilter = 0;
```

New input handler:

```js
function onMinSpeedInput(value) {
  minSpeedFilter = parseFloat(value);
  document.getElementById('min-speed-label').textContent = Math.round(minSpeedFilter) + ' mph';
  roadLayer.setStyle(segmentStyle);
}
```

### `segmentStyle` update

In the `OVERLAY_ROADS` branch, after the existing `minScoreFilter` check, add:

```js
if (minSpeedFilter > 0) {
  var spd = feature && feature.properties ? feature.properties.max_speed_mph : null;
  if (spd === null || spd === undefined || spd < minSpeedFilter) {
    return { opacity: 0, weight: 0 };
  }
}
```

Roads with `max_speed_mph: null` (no tagged speed) are hidden when `minSpeedFilter > 0`, shown when at 0.

### `toggleOverlayMode` update

Where the min-twist slider is enabled/disabled on mode toggle, apply the same to the min-speed slider:

```js
document.getElementById('min-speed-slider').disabled = overlayMode === OVERLAY_WAYS;
```

## Filtering semantics

| Slider value | Road has speed tag | Shown? |
|---|---|---|
| 0 | any / none | yes (no filter active) |
| > 0 | yes, speed >= minSpeedFilter | yes |
| > 0 | yes, speed < minSpeedFilter | no |
| > 0 | no (null) | no |

Min-twist AND min-speed: both conditions must pass independently.

## Testing

- Unit test for `WeightedAverageSpeedMPH`: empty slice returns `(0, false)`; single entry returns its speed; mixed tagged/untagged entries weight only tagged ones; km/h entries are already converted by `ParseMaxspeed` before reaching this function.
- Existing `collectionsToRoadGeoJSON` tests (if any) should be extended to assert `max_speed_mph` is set correctly.
- Manual verification: load build UI, switch to Road overlay, drag min-speed slider, confirm roads below threshold disappear; confirm roads with no speed tag disappear above 0; confirm min-twist and min-speed both filter independently.
