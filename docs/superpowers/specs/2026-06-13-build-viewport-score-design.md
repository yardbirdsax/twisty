# Viewport Score Refresh

**Date:** 2026-06-13  
**Branch:** feat/route-build

## Summary

Add a "Refresh score" button to the stats panel that rescores all tiles visible in the current map viewport, independent of the current route.

## Backend

### New endpoint: `POST /api/score/viewport`

Registered in `execBuild` alongside existing routes.

**Request body:**
```json
{ "west": float, "south": float, "east": float, "north": float }
```

**Handler behavior:**
1. Compute intersecting tiles using existing `tilesInBBox(west, south, east, north)` helper (line 1156 in `route_build.go`)
2. Partition into cached vs. missing tiles (same logic as `handleScore`)
3. If missing tiles exist, kick off `go s.fetchMissingTiles(missingTiles)`
4. Score cached tiles via `scoreFromCachedTiles`
5. Count failed tiles from `s.failedTiles`
6. Return `scoreResponse{Score, PendingTiles, FailedTiles}` — same shape as `/api/score`

No new types needed. No change to existing endpoints.

## Frontend

### Button

A "Refresh score" button added to the stats panel row alongside Save/Load. Always enabled (no route required). Styled the same as existing stat panel buttons.

### `requestViewportScore()` function

On click, calls `requestViewportScore()` which:
1. Gets current map bounds via `map.getBounds()`
2. POSTs `{ west, south, east, north }` to `/api/score/viewport`
3. Updates `score-val` and `fetch-status` identically to `requestScore()`
4. If `pending_tiles > 0`, schedules itself to re-run in 2000ms (same poll loop as `requestScore()`)

The existing `scorePollTimer` is reused/shared — calling either function cancels any in-flight poll.

## Error handling

Same as existing score flow: on fetch error, sets `fetch-status` to `'Score error'`.

## Testing

- Unit test for new `handleViewportScore` handler: valid bbox returns `scoreResponse`, missing tiles trigger async fetch, invalid JSON returns 400
- Frontend behavior covered by existing test patterns (served HTML presence checks)
