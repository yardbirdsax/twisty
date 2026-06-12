# Design: `twisty build` State Persistence via localStorage

**Date:** 2026-06-12
**Status:** Approved

## Problem

Refreshing the browser while using `twisty build` clears all state — the map viewport, waypoints, and routed legs reset to the server-provided starting point. Users lose their in-progress route on every refresh.

## Solution

Persist the route state (viewport, waypoints, leg geometry) in `localStorage` under a single key. On page load, restore from localStorage instead of the server-provided defaults. The score is not persisted — it is re-requested via `/api/score` after restore.

No server changes are required.

## Saved State Shape

Key: `twisty-build-state`

```json
{
  "zoom": 13,
  "center": [40.123, -75.456],
  "waypoints": [[40.1, -75.4], [40.2, -75.5]],
  "legs": [
    { "points": [[40.1, -75.4], [40.15, -75.45]], "duration": 312, "distance": 4200 }
  ]
}
```

The `legs` array matches the existing in-memory `legs` structure exactly, so serialization is a straight `JSON.stringify`.

## When State Is Saved

A `saveState()` helper serializes the current `waypoints`, `legs`, and map viewport to `localStorage`. It is called:

- After a leg is successfully routed (inside the `/api/route-leg` `.then()`, after `legs.push(...)`)
- After a waypoint is added with no previous point (no leg yet, but viewport + single waypoint should be saved)
- After `removeLastWaypoint()` completes
- On map `moveend` and `zoomend` (so panning without waypoints still restores viewport)

When `waypoints` reaches length 0 after `removeLastWaypoint()`, `localStorage.removeItem('twisty-build-state')` is called instead of saving an empty state.

## Restore on Page Load

The map currently initializes with a Go-templated center/zoom: `map.setView([%f, %f], 13)`. This becomes the fallback when no saved state exists.

Restore sequence on page load:

1. Read and parse `localStorage.getItem('twisty-build-state')`; if absent or unparseable, use server defaults and stop
2. Call `map.setView(saved.center, saved.zoom)`
3. Set `waypoints = saved.waypoints`, `legs = saved.legs`
4. For each leg, create an `L.polyline` from `leg.points` and push it into `legPolylines`, add to map
5. Call `refreshMarkers()` to render waypoint pins
6. Call `updateStats()` to populate distance/time display
7. Call `requestScore()` to fetch the score for the restored route

## Clear Route Button

A "Clear route" button is added to the control bar alongside the Export buttons. It is disabled when `waypoints` is empty.

On click:
1. Remove all leg polylines from the map and reset `legPolylines = []`
2. Remove all markers from the map and reset `markers = []`
3. Reset `waypoints = []`, `legs = []`
4. Call `localStorage.removeItem('twisty-build-state')`
5. Call `updateStats()` and `requestScore()` to reset score/stats display

## Error Handling

- If `localStorage` is unavailable (private browsing, storage quota exceeded): `saveState()` wraps the write in a `try/catch` and silently ignores the error. The app continues to function without persistence.
- If the saved JSON is malformed on restore: catch the parse error, discard the entry with `localStorage.removeItem`, and fall back to server defaults.

## Testing

- Manual: add waypoints, refresh — route and viewport restore. Pan without waypoints, refresh — viewport restores. Clear route, refresh — starts fresh.
- No automated tests needed for this feature (pure frontend localStorage logic, no new server endpoints).
