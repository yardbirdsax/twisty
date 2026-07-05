# Waypoint Reorder and Insert — Design Spec

**Date:** 2026-07-05

## Overview

Add drag-and-drop reordering and positional insertion to the waypoint list panel in `twisty build`. Users can grab a drag handle on any waypoint row to reorder it, and drag the `+ Add waypoint` row to insert a new waypoint at a specific position in the list.

## Scope

Frontend-only. No new backend endpoints. All changes are in the inline HTML/JS inside `route_build.go`.

## UI / Interaction

### Waypoint row drag handle

Each waypoint row (`wp-row`) gains a `⠿` handle on the left:

- Styled muted gray, `cursor: grab` / `cursor: grabbing` while dragging
- Only the handle element is the `draggable` source — the address label and badge retain their existing click behaviors (edit and active-slot)
- The dragged row gets `opacity: 0.4` while in flight
- A 2px solid blue horizontal line renders between rows as the active drop target indicator (not a full-row highlight)

### `+ Add waypoint` row drag

The `+ Add waypoint` row (`#waypoints-add`) gains the same `⠿` handle and is `draggable`.

- Dragging it and dropping between existing rows (or at the top/bottom of the list) sets an **insert index**
- After drop, the address input opens inline at that position — same UX as the current `+ Add waypoint` click flow, but the new waypoint will be spliced in at the chosen index rather than appended
- Pressing Escape or a geocode failure cancels the insert; `waypoints` is unchanged

### Drop indicator

A thin `<div class="wp-drop-indicator">` element (2px, blue, `#2563eb`) is absolutely positioned between rows during a drag. It appears between the row above and below the current `dragover` target. It is removed on `dragend` or `drop`.

## Data Flow

### Reorder

1. `dragstart` on a handle captures the source row index
2. `dragover` on the list computes the target index from mouse Y position and renders the drop indicator
3. `drop` splices `waypoints` to the new order
4. Clears all `legPolylines` from the map, resets `legs = []`
5. Calls `rerouteAll()` — rebuilds all legs sequentially from the updated `waypoints`
6. Calls `saveState()` — persists to localStorage

If source index === computed target index, the operation is a no-op (no rerouteAll, no saveState).

### Insert

1. `dragstart` on `#waypoints-add` handle marks it as an insert drag
2. `dragover` on the list renders the drop indicator at the target position
3. `drop` records the insert index, then opens the address input inline at that position (a temporary placeholder row)
4. On geocode success: splices `[lat, lon]` into `waypoints` at insert index, caches the label, calls `rerouteAll()` and `saveState()`
5. On geocode failure or Escape: removes the placeholder row, no state change

`labelCache` requires no changes — it is keyed by `"lat,lon"` strings, so entries survive reorder automatically.

## Error Handling

| Scenario | Behavior |
|---|---|
| Drop onto same position | No-op — skip rerouteAll and saveState |
| Drop row onto itself | No-op |
| Insert geocode fails | Placeholder row removed; shake + inline error (existing behavior) |
| Insert cancelled (Escape) | Placeholder row removed; no state change |
| rerouteAll leg fails | Existing toast ("Could not route leg N") — no new handling |

## CSS additions

```css
.wp-handle { color: #9ca3af; cursor: grab; font-size: 13px; flex-shrink: 0; padding: 0 2px; user-select: none; }
.wp-handle:active { cursor: grabbing; }
.wp-row.dragging { opacity: 0.4; }
.wp-drop-indicator { height: 2px; background: #2563eb; margin: 1px 0; border-radius: 1px; }
```

## Testing

- Manual: drag a middle waypoint to first position; verify route rebuilds correctly
- Manual: drag `+ Add waypoint` between waypoints 1 and 2; type an address; verify new waypoint appears at index 1 and route rebuilds
- Manual: drag a row to its current position; verify no re-route fires
- Manual: drag `+ Add waypoint`, drop, then press Escape; verify list is unchanged
- Existing Go tests unaffected (no backend changes)

## Out of Scope

- Touch/mobile drag support
- Keyboard reordering (arrow keys)
- Delete waypoint from panel (still via "click last marker to undo" or Clear)
- Autocomplete in the address input
