# Build screen: center map on typed waypoint addresses

## Problem

Entering an address as a waypoint on the build screen geocodes it and updates
the waypoint list, markers and route, but the map view does not move. If the
address is off-screen the user has to pan to it manually.

## Behavior

When a waypoint address is entered by typing and the geocode succeeds, the map
recenters on the geocoded point:

- Applies to all three typed-address paths in `static/build.js`:
  `commitWaypointEdit`, `commitAddWaypoint`, `commitInsertWaypoint`.
- Does **not** apply to map clicks, marker drags, or loading a saved route.
- Zoom: if the current zoom is below 14 (street level), zoom in to 14.
  If already at 14 or closer, keep the current zoom. Never zoom out.
- Geocode failures (404 / error) do not move the map.

## Design

Add one helper in `static/build.js`:

```js
var ADDRESS_MIN_ZOOM = 14;

function centerOnAddress(lat, lon) {
  map.setView([lat, lon], Math.max(map.getZoom(), ADDRESS_MIN_ZOOM));
}
```

Each of the three commit handlers calls `centerOnAddress(data.lat, data.lon)`
in its success branch. The existing `moveend`/`zoomend` listener reloads
visible road segments for the new view; no extra wiring is needed.

## Testing (red-green, `static/build.test.js`)

For each of the three typed-address paths:

- success at zoom < 14 → map is set to the geocoded point at zoom 14
- success at zoom >= 14 → map is centered on the point, zoom unchanged
- geocode failure → map view unchanged

And: a map click that adds a waypoint does not change the view.

## Out of scope

- A standalone search box.
- Changing viewport behavior on route load or marker drag.
