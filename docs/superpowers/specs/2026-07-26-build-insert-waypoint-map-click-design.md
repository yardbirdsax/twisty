# Design: Insert waypoint via map click

**Date:** 2026-07-26
**Status:** Approved

## Background

The waypoint list already supports inserting a waypoint at a specific position by dragging the `⠿` handle on the "Add waypoint" row to between two existing rows. This triggers `startInsertWaypoint(toIndex)`, which shows a placeholder row with a text input accepting a typed address.

Users also want to click on the map to place the inserted waypoint, the same way they can for appending (map click → `addWaypoint`) and for replacing an active slot (`activeSlotIndex`).

## Approach

Mirror the existing `activeSlotIndex` pattern. Add a parallel `insertSlotIndex` variable that is set when an insert placeholder is active. The map click handler checks `insertSlotIndex` before the existing `activeSlotIndex` branch, and if set, inserts the clicked latlng at that position directly (no geocode round-trip needed).

## State Changes

Add two module-level variables alongside `activeSlotIndex`:

```js
var insertSlotIndex = -1;        // target insert position, or -1 when idle
var insertPlaceholderRow = null; // reference to the DOM placeholder row
```

`insertPlaceholderRow` is needed so the map click handler can remove it from the DOM without re-querying.

## `startInsertWaypoint` changes

- After inserting the placeholder row and focusing the input, set `insertSlotIndex = insertIndex` and `insertPlaceholderRow = placeholderRow`.
- On Escape, clear both back to `-1` / `null`.

## `commitInsertWaypoint` changes

- Clear `insertSlotIndex = -1` and `insertPlaceholderRow = null` at the start of the `.then()` body (before any DOM work), so a concurrent map click cannot double-insert.

## Map click handler

In `addWaypoint(latlng)`, add a branch before the existing `activeSlotIndex` check:

```js
if (insertSlotIndex >= 0) {
  var idx = insertSlotIndex;
  var row = insertPlaceholderRow;
  insertSlotIndex = -1;
  insertPlaceholderRow = null;
  if (row && row.parentNode) row.parentNode.removeChild(row);
  waypoints.splice(idx, 0, [latlng.lat, latlng.lng]);
  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  refreshMarkers();
  renderWaypointList();
  rerouteAll();
  saveState();
  return;
}
```

No geocode call — the latlng is already known. Reverse geocoding (for the label) is handled lazily by `reverseGeocodeUnlabeled()` when the waypoints panel is open; `rerouteAll` doesn't need a label.

## No change to existing paths

- `activeSlotIndex` replace logic in `addWaypoint` is untouched.
- `commitInsertWaypoint` address path is untouched (still geocodes and splices normally).
- Drag-to-reorder is untouched.

## Testing

New test cases in `build.test.js`:

1. When `insertSlotIndex >= 0`, a map click inserts the latlng at the correct index and calls `rerouteAll`.
2. After the map click, `insertSlotIndex` is reset to `-1`.
3. Escape in the insert placeholder clears `insertSlotIndex` so a subsequent map click falls through to the normal append path.
4. If `commitInsertWaypoint` resolves after a map click has already consumed the slot, it does not double-insert (cleared state prevents it).
