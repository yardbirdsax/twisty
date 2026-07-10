# Min Twist Slider for Road Overlay

**Date:** 2026-07-09
**Status:** Approved

## Summary

Add a "Min twist" slider to the stats panel that filters which roads are visible in Road overlay mode by their total curvature score. The slider has no effect in Ways (segment) mode and is disabled/greyed out there. All road data is always fetched; filtering is purely client-side via Leaflet's `setStyle`.

## UI

A new row is added to the stats panel between the overlay toggle button and the Save/Load row:

```
Min twist: 1200
[========--------]  ← range input, full panel width
```

- Label shows "Min twist: N" where N updates live as the slider moves
- Range: 0–8000 (matching `DefaultMaxCurvature`), step 100
- Default value: 0 (show all roads)
- Disabled and visually greyed out when `overlayMode === OVERLAY_WAYS`
- Enabled when `overlayMode === OVERLAY_ROADS`

## Filtering Logic

A `minScoreFilter` variable (default `0`) is set by the slider's `input` event handler.

The existing `segmentStyle(feature)` function gains one additional check:

```js
if (overlayMode === OVERLAY_ROADS && feature.properties.score < minScoreFilter) {
  return { opacity: 0, weight: 0 };
}
```

This check is skipped entirely in Ways mode, so all way segments are always fully visible regardless of `minScoreFilter`.

After updating `minScoreFilter`, the handler calls `roadLayer.setStyle(segmentStyle)` to re-apply styles to all features already in the layer. No network request is made.

The `score` property is already present on road GeoJSON features (`geoJSONRoadProps.Score`) — no backend changes are required.

## Mode Switching

- Switching to **Ways mode**: slider is disabled (greyed out). `minScoreFilter` retains its current value.
- Switching to **Roads mode**: slider is re-enabled with its previous value. `roadLayer.setStyle(segmentStyle)` is called so the filter is immediately applied to the current road features.
- Pan/zoom always fetches all roads regardless of slider value; `mergeFeatures` adds them all to the layer, and the style function hides those below threshold automatically.

## No Backend Changes

All filtering is client-side. The `score` field on road features is the total curvature score (`RoadCollection.TotalScore`), which is already serialised by `collectionsToRoadGeoJSON`. No API changes, no new query parameters.

## Testing

- Manual: verify slider hides/shows roads instantly without network requests in Road mode
- Manual: verify slider has no effect in Ways mode
- Manual: verify switching modes preserves slider value and re-applies filter on return to Roads mode
- No new Go tests required (no backend changes)
