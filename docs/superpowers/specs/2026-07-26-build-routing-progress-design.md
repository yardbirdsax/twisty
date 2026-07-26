# Build: Routing Progress Indicator

**Date:** 2026-07-26
**Status:** Approved

## Overview

When the build UI calculates routes between waypoints, there is currently no progress indicator showing how many legs remain. This spec adds "Routing leg N of M..." messages to the existing `routing` slot in the status bar for all routing scenarios.

## Scope

Only `static/build.js` changes. No changes to `statusManager.js`, `build.html`, or any Go server files.

## Design

### Status message format

```
Routing leg N of M...
```

Where N is the 1-based index of the leg currently being fetched, and M is the total number of legs to fetch in the current routing operation.

### Single-leg routing (`addWaypoint`)

When the user clicks the map to add a waypoint, exactly one leg is routed. The existing `statusManager.set('routing', 'Routing...', false)` call is replaced with:

```
statusManager.set('routing', 'Routing leg 1 of 1...', false)
```

On success, `statusManager.clear('routing')` is called as before. On error, `statusManager.clear('routing')` is called as before.

### Multi-leg routing (`rerouteAll`)

`rerouteAll` routes all legs sequentially via the recursive `routeNext(i)` helper. Before each fetch, the status is updated to reflect the current leg:

```
statusManager.set('routing', 'Routing leg ' + (i+1) + ' of ' + (waypoints.length - 1) + '...', false)
```

This call is placed at the top of `routeNext(i)`, before the `fetch`. When the last leg completes successfully, `statusManager.clear('routing')` is called. On error for any leg, `statusManager.clear('routing')` is called (the toast handles user-facing error messaging).

Currently `rerouteAll` has no statusManager calls at all; this adds them.

### Generation guard

`rerouteAll` already uses a `routingGeneration` counter to discard stale callbacks. The status update calls are gated on `gen !== routingGeneration` checks in the same way as the existing polyline/leg logic, so a superseded reroute does not update the status bar.

The `clear` on the final leg is also gated: only the last leg of the current generation clears the slot.

## Testing

The existing Node test suite in `build.test.js` covers `statusManager` interactions. New tests will cover:

- Single-leg: status set to "Routing leg 1 of 1..." on start, cleared on success, cleared on error
- Multi-leg (3 waypoints, 2 legs): status cycles through "Routing leg 1 of 2...", "Routing leg 2 of 2...", then cleared
- Stale generation: a new `rerouteAll` call while a previous one is in-flight does not corrupt status from the stale callbacks

## Out of scope

- Percentage or progress bar UI
- Showing elapsed time or ETA
- Changes to scoring or roads progress indicators
