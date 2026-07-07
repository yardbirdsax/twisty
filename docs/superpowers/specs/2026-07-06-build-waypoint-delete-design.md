# Build: Waypoint Delete

**Date:** 2026-07-06
**Status:** Approved

## Problem

The waypoint list in the build UI supports adding, editing, reordering, and inserting waypoints, but has no way to remove an individual waypoint from an arbitrary position in the list.

## Design

### UI

Each `.wp-row` gets a `×` delete button at the far right. It is always visible and turns red on hover to signal its destructive nature.

New CSS:

```css
.wp-delete { color: #9ca3af; font-size: 11px; flex-shrink: 0; cursor: pointer; padding: 0 2px; }
.wp-delete:hover { color: #dc2626; }
```

The button is appended to the row in `renderWaypointList()` after the label element, using `e.stopPropagation()` to prevent click bubbling.

### Behavior

Clicking `×` on row `i` calls `removeWaypoint(i)`:

1. Splice waypoint `i` out of `waypoints`.
2. Remove all leg polylines from the map and clear `legPolylines` and `legs`.
3. Call `refreshMarkers()` and `renderWaypointList()`.
4. If no waypoints remain: call `updateStats()` and `requestScore()` and remove the localStorage entry.
5. If waypoints remain: call `rerouteAll()` (which re-fetches all legs, updates stats/score, and saves state) then `saveState()`.

### Edge Cases

- **Delete only waypoint:** leaves an empty route; export buttons gray out, score shows `—`.
- **Delete first or last waypoint:** `rerouteAll()` re-routes the remaining pairs correctly — no special case needed.
- **`activeSlotIndex` pointing at or beyond the deleted index:** `renderWaypointList()` redraws from scratch and `rerouteAll()` does not use `activeSlotIndex`, so no stale state can accumulate.

### Consistency with existing patterns

Clearing legs and calling `rerouteAll()` is identical to how drag-to-reorder works, keeping behavior consistent across all mutation operations on the waypoint list.
