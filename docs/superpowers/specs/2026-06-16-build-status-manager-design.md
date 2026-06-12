# Status Manager Design

**Date:** 2026-06-16
**Branch:** feat/route-build

## Problem

The `#fetch-status` DOM element in the build UI is updated by 22 scattered `textContent`/`className` assignments across three async concerns: routing, scoring, and road segment loading. There is no indication to the user when road segments are being loaded after panning/zooming. Updates from different concerns silently overwrite each other with no coordination.

## Goal

- Show progress feedback when road scores are being calculated (pan/zoom/recalculate)
- Consolidate status updates behind a single manager so new async concerns can be added without scattering new DOM assignments
- Add unit tests for the status logic

## Architecture

### Status Manager (`static/statusManager.js`)

A `createStatusManager(el)` factory function takes a DOM element (or mock) and returns a manager object. The manager maintains a priority-ordered map of named slots:

| Slot | Priority | Concern |
|------|----------|---------|
| `routing` | 1 (highest) | Route leg fetch |
| `scoring` | 2 | Score and viewport score requests |
| `roads` | 3 (lowest) | Road segment tile loading on pan/zoom |

Each slot holds either `null` (inactive) or `{ text: string, done: boolean }`.

**Public API:**
- `statusManager.set(slot, text, done)` — activate a slot with a message
- `statusManager.clear(slot)` — deactivate a slot

**Internal:**
- `render()` — iterates slots in priority order, picks the first non-null, updates `el.textContent` and `el.className` (`fetch-status` or `fetch-status done`)
- Idle/default state: when all slots are null, renders "Click map to start" with `done` class

### Embedding (`route_build.go`)

`static/statusManager.js` is embedded into the Go binary via `//go:embed static/statusManager.js`. The `buildHTML` template gains a `%s` placeholder for the `<script>` block. `handleIndex` passes the embedded JS when formatting the HTML string.

### Callers

Each async concern calls `set`/`clear` instead of directly touching the DOM:

- `addWaypoint()` → `set('routing', 'Routing...')` on fetch start, `clear('routing')` on resolve/error
- `requestScore()` / `requestViewportScore()` → `set('scoring', ...)` / `clear('scoring')`
- `loadVisibleSegments()` → `set('roads', '⏳ Loading roads...')` on fetch start, `clear('roads')` on resolve/error

### Priority Behavior

When multiple slots are active simultaneously, the highest-priority slot wins. Example: if routing and road loading are both in-flight, the user sees "Routing..." until routing resolves, then sees "Loading roads..." if that fetch is still pending.

## Testing

### Unit tests (`static/statusManager.test.js`)

Uses Node's built-in `node --test` (Node 18+, no npm/package.json required). Each test creates a mock element `{ textContent: '', className: '' }` and asserts on it after API calls. Cases:

- Set `scoring` only → shows scoring message, `fetch-status` class
- Set `routing` and `scoring` → shows routing message (higher priority)
- Clear `routing` while `scoring` active → shows scoring message
- Set `roads` while `scoring` active → shows scoring message
- Clear all slots → shows "Click map to start" with `fetch-status done` class
- `done: true` → `fetch-status done` class; `done: false` → `fetch-status` class

### Makefile

`make test` gains a second line:

```makefile
test:
	go test -short ./...
	node --test static/statusManager.test.js
```

## Files Changed

| File | Change |
|------|--------|
| `static/statusManager.js` | New — status manager factory |
| `static/statusManager.test.js` | New — unit tests |
| `route_build.go` | Embed JS, add `%s` to `buildHTML`, update `handleIndex`, replace 22 DOM assignments with manager calls, add `loadVisibleSegments` status calls |
| `Makefile` | Add `node --test` line to `test` target |
