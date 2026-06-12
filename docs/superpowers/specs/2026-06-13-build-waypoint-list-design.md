# Waypoint List Panel — Design Spec

**Date:** 2026-06-13
**Branch:** feat/route-build

## Overview

Add a collapsible waypoint list to the `#stats` panel in the build UI. Each waypoint shows a resolved address (reverse-geocoded lazily) or falls back to `lat, lon`. Waypoints are editable inline — clicking a row turns it into a text input for forward geocoding. A new `+ Add waypoint` row at the bottom allows appending waypoints by address without clicking the map.

## UI Layout

The `#stats` panel gains a "Waypoints" section appended below the existing buttons and status line, separated by a horizontal rule.

**Collapsible header:** Shows "Waypoints (N)" with a toggle arrow (▶ collapsed, ▼ expanded). Collapsed by default when there are no waypoints. Auto-expands when the first waypoint is added.

**When expanded, the list contains:**

1. One row per waypoint — a numbered badge (①②③…) and a resolved address or `lat, lon` fallback.
2. A `+ Add waypoint` row at the bottom.

**Inline editing:** Clicking any waypoint row or the `+ Add waypoint` row turns it into a text input. The user types an address and presses Enter to geocode. On success, the waypoint is updated (or appended) and the row returns to display mode showing the resolved name. Pressing Escape cancels the edit and restores the previous label.

**Active slot selection:** Clicking the numbered badge (①②…) on a row — not the address text — marks it as the active slot, highlighted with a blue left border. The next map click replaces that waypoint instead of appending a new one. Clicking the address text enters edit mode instead. Clicking the map (whether to set the active slot's position or anywhere else) deselects the active slot.

## Data Model

The `waypoints` JS array and save file format are **unchanged** — still `[[lat, lon], ...]`. Address labels are ephemeral UI state only, cached in a JS `Map<string, string>` keyed by `"lat,lon"` for the session lifetime. Labels are never persisted to the save file.

## Backend — New Endpoints

### `GET /api/geocode?q=<address>`

Proxies a forward geocode request to Nominatim using the existing `geocode` package. Returns:

```json
{"lat": 40.1234, "lon": -76.5678, "display_name": "123 Main St, Pottsville, PA"}
```

Returns HTTP 404 with a JSON error if no results are found. Returns HTTP 500 on network or parse errors.

### `GET /api/reverse-geocode?lat=<lat>&lon=<lon>`

Proxies a reverse geocode request to Nominatim. Returns:

```json
{"display_name": "123 Main St, Pottsville, PA"}
```

Falls back gracefully — if Nominatim returns no result or errors, the caller displays `lat, lon` instead. Returns HTTP 200 with an empty `display_name` on no-result (not an error).

## Reverse Geocoding Strategy

When the waypoints panel is first expanded, the browser fires reverse geocode requests for any waypoints that don't yet have a cached label. Requests are fired **sequentially** (one at a time) to respect Nominatim's rate limit policy. Labels are written into the cache as each response arrives and the corresponding row updates in place.

## Error Handling

| Scenario | Behavior |
|---|---|
| Forward geocode: no results | Input shakes, shows inline "Address not found" message; input stays open |
| Forward geocode: network error | Same as no results |
| Reverse geocode: no results | Row silently displays `lat, lon` |
| Reverse geocode: network error | Row silently displays `lat, lon` |
| Escape pressed during edit | Edit cancelled, previous label restored |

## Testing

- Unit test `/api/geocode` handler: mock Nominatim HTTP, assert correct lat/lon/display_name returned; assert 404 on empty results.
- Unit test `/api/reverse-geocode` handler: mock Nominatim HTTP, assert correct display_name returned; assert empty display_name (not error) on no results.
- HTML render test in `route_build_test.go`: assert the waypoints collapsible section is present in the rendered HTML (header row, toggle arrow, `+ Add waypoint` row).
- JS interaction (inline editing, label caching, active-slot highlight) tested manually.

## Out of Scope

- Persisting address labels to the save file
- Drag-to-reorder waypoints
- Deleting waypoints from the panel (still done via "Click last marker to undo" or Clear)
- Autocomplete/suggestions while typing in the address input
