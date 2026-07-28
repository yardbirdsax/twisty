# Build UI Preferences Persistence

**Date:** 2026-07-27  
**Status:** Approved

## Problem

UI settings on the build page (overlay mode, min twist, min speed, overlay visibility, waypoints panel state) are lost on page reload. Waypoint reverse-geocode labels are also re-fetched unnecessarily. These should survive a reload.

## What Gets Persisted

Two localStorage keys:

### `twisty-ui-prefs` (new)

View preferences — independent of route state. Survives route clear.

```json
{
  "overlayMode": "ways",
  "overlayVisible": true,
  "minTwistSlider": 0,
  "minSpeedSlider": 0,
  "waypointsPanelOpen": false
}
```

Slider positions stored as raw 0–100 integers matching the `<input>` value, not computed filter values. On restore, the slider value is set and the existing `onMinTwistInput`/`onMinSpeedInput` handlers recompute `minScoreFilter`/`minSpeedFilter` and update labels.

### `twisty-build-state` (extended)

Add `labelCache` to the existing route state object. Lifecycle is already correct: `clearRoute()` removes the key, taking the cache with it.

```json
{
  "zoom": 13,
  "center": [37.7, -122.4],
  "waypoints": [[37.7, -122.4], [37.8, -122.3]],
  "legs": [...],
  "labelCache": {
    "37.700000,-122.400000": "Main St, San Francisco, CA"
  }
}
```

## New Functions

**`savePrefs()`** — serializes current `overlayMode`, `overlayVisible`, `minTwistSlider` (read from the DOM input), `minSpeedSlider` (read from the DOM input), and `waypointsPanelOpen` to `twisty-ui-prefs`. Wrapped in try/catch like `saveState()`.

**`loadPrefs()`** — reads `twisty-ui-prefs` and applies each setting:
- Sets `overlayMode` and updates button label
- Calls `toggleOverlayVisibility` logic if `overlayVisible` is false (without re-saving)
- Sets slider values and calls `onMinTwistInput`/`onMinSpeedInput` to recompute filters and update labels
- Sets `waypointsPanelOpen` and updates panel DOM

Missing or invalid keys fall back to current defaults gracefully (no removal of the key on partial parse failure, since prefs are not safety-critical like route state).

## Wiring

Each of these calls `savePrefs()` at the end:

- `toggleOverlayMode`
- `toggleOverlayVisibility`
- `onMinTwistInput`
- `onMinSpeedInput`
- `toggleWaypointsPanel`

`saveState()` is extended to include `labelCache` in the serialized object. `restoreState()` is extended to read `labelCache` from the parsed state and assign it to the global.

## Startup Order

```
loadPrefs()       ← sets overlayMode before any segment fetch
restoreState()    ← restores route, then calls loadVisibleSegments()
```

`loadPrefs()` must run before `restoreState()` because `overlayMode` controls which API endpoint `loadVisibleSegments` uses. If prefs load after, the first segment fetch uses the wrong mode.

## Out of Scope

- `activeSlotIndex` / `insertSlotIndex` — transient interaction state, confusing to restore
- Score display — derived, re-requested on load already
- No migration logic needed — new keys, no conflict with existing state
