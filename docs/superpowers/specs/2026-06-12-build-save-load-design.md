# Design: Route Save/Load for `twisty build`

**Date:** 2026-06-12
**Status:** Approved

## Overview

Add Save and Load buttons to the `twisty build` map UI so users can preserve and restore route-building sessions across multiple working routes. The feature is entirely frontend-only — no new backend endpoints.

## File Format

Routes are saved as `.twisty.json` files with the following shape:

```json
{
  "version": 1,
  "viewport": { "center": [40.1, -75.3], "zoom": 13 },
  "waypoints": [[40.1, -75.3], [40.2, -75.4]],
  "legs": [
    { "points": [[40.1, -75.3], [40.15, -75.35]], "duration": 312, "distance": 4.2 }
  ]
}
```

- `version`: integer, currently `1`. Reserved for future format evolution.
- `viewport.center`: `[lat, lng]` pair matching Leaflet's `map.getCenter()`.
- `viewport.zoom`: integer zoom level.
- `waypoints`: array of `[lat, lng]` pairs, one per map marker.
- `legs`: array of leg objects, each with `points` (array of `[lat, lng]`), `duration` (seconds), and `distance` (miles). Mirrors the existing `legs` array stored in `localStorage`.

This shape deliberately matches the existing `saveState()` / `restoreState()` localStorage format so that load can reuse the restoration logic almost verbatim.

## UI

Two buttons are added as a new row inside the existing `#stats` panel (top-right), between the overlay toggle row and the fetch status line:

```html
<div class="row" style="margin-top:4px;gap:4px;display:flex;">
  <button onclick="saveRoute()" style="flex:1;...">Save</button>
  <button onclick="loadRoute()" style="flex:1;...">Load</button>
</div>
```

Buttons use the same styling as the existing overlay toggle (dark background, white text, full-width row).

## Behavior

### Save

`saveRoute()`:
1. Reads current `map.getCenter()`, `map.getZoom()`, `waypoints`, and `legs`.
2. Serializes to JSON with `version: 1`.
3. Creates a temporary `<a>` element with `href = URL.createObjectURL(blob)` and `download = "route.twisty.json"`, clicks it, then revokes the object URL.

No server involvement. Always enabled (even with no waypoints — produces a valid file with empty arrays).

### Load

`loadRoute()`:
1. Programmatically clicks a hidden `<input type="file" accept=".json">`.
2. On file selection, reads the file via `FileReader.readAsText`.
3. Validates the parsed object:
   - Must have `version === 1`
   - `Array.isArray(waypoints)` and `Array.isArray(legs)`
   - Each leg must have `Array.isArray(leg.points)`
   - `waypoints.length <= legs.length + 1`
4. On validation failure: shows a toast ("Invalid route file") and leaves current state untouched.
5. On success: calls `clearRoute()` to reset all current state, then restores `waypoints`, `legs`, viewport, polylines, markers, stats, and triggers score polling — mirroring `restoreState()`.

## Error Handling

| Scenario | Behavior |
|---|---|
| File is not valid JSON | Toast: "Invalid route file", state unchanged |
| File fails schema validation | Toast: "Invalid route file", state unchanged |
| Load with route in progress | `clearRoute()` first, then restore — no confirmation dialog |
| Save with empty route | Downloads valid file with empty `waypoints: []` and `legs: []` |

## What Does NOT Change

- `/api/segments`, `/api/road-segments`, `/api/segments/stream` are untouched.
- The localStorage auto-save/restore (`saveState` / `restoreState`) continues to work as before.
- Export (GPX/KML), Clear route, overlay toggle, routing, and score polling are untouched.
- No new Go backend code is needed.
