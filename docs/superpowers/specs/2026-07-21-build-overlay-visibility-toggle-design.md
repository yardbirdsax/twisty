# Overlay Visibility Toggle

**Date:** 2026-07-21
**Status:** Approved

## Summary

Add a "Hide overlay" / "Show overlay" button to the stats panel that toggles the colored road/way overlay on and off without recalculating or refetching data. The overlay mode toggle and min twist slider remain functional while the overlay is hidden.

## UI

A new button row is added to the stats panel above the existing overlay mode toggle row:

```
[  Hide overlay  ]   ← new button
[Switch to Road view]
Min twist: 0
[=================]
```

The button reads "Hide overlay" when the overlay is visible and "Show overlay" when hidden. Styling matches the existing panel buttons (dark background, white text, full width).

## State

A single `var overlayVisible = true` boolean tracks visibility. It is independent of `overlayMode` and `minScoreFilter` — those retain their values and remain interactive while the overlay is hidden.

## Toggle Logic

`toggleOverlayVisibility()`:
- If `overlayVisible`: call `map.removeLayer(roadLayer)`, set `overlayVisible = false`, update button text to "Show overlay"
- If not `overlayVisible`: call `map.addLayer(roadLayer)`, set `overlayVisible = true`, update button text to "Hide overlay"

Using `map.removeLayer` / `map.addLayer` (Leaflet-idiomatic) rather than `opacity: 0` avoids the invisible-feature hit-testing problem.

## Edge Case: rebuildSegmentLayer

`rebuildSegmentLayer()` creates a new layer and unconditionally calls `.addTo(map)`. If the overlay is hidden and the user switches modes (which calls `rebuildSegmentLayer`), the new layer would reappear.

Fix: change `.addTo(map)` in `rebuildSegmentLayer` to conditionally add only when `overlayVisible === true`:

```js
roadLayer = L.geoJSON(null, { style: segmentStyle });
if (overlayVisible) roadLayer.addTo(map);
```

## No Backend Changes

Purely client-side display toggle. No data is discarded; re-showing the overlay is instant.

## Testing

- Manual: verify "Hide overlay" removes colored lines from map without network requests
- Manual: verify "Show overlay" instantly restores lines with current mode and min twist filter applied
- Manual: verify overlay mode toggle and min twist slider remain functional while overlay is hidden
- Manual: verify switching overlay modes while hidden does not make the overlay reappear
